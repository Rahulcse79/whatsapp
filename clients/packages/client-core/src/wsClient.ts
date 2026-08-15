// The realtime connection: handshake, resume, heartbeat, and reconnect/backoff
// (websocket-protocol.md §1, §6). Transport- and timer-agnostic — every effect
// goes through an injected port — so the whole state machine is unit-tested in
// Node with a fake transport and a manual clock.
//
// Durability does NOT live here. UnACKed sends are re-fetched from the outbox
// on every live transition and retransmitted (the server dedupes by msg_uuid),
// and inbox batches are persisted BEFORE ClientAck. A dropped socket therefore
// never loses or duplicates a message; it just reconnects and resumes.

import { backoffDelay, defaultBackoff, type BackoffConfig } from "./backoff";
import {
  CloseCode,
  ErrorCode,
  type CallEnd,
  type CallOffer,
  type CallRing,
  type ChannelEvent,
  type ClientFrame,
  type ConversationCursor,
  type HelloAck,
  type InboxBatch,
  type MsgAck,
  type MsgSend,
  type PresenceUpdate,
  type Receipt,
  type ServerError,
  type ServerFrame,
  type ServerHint,
  type Typing,
} from "./frames";
import type { Cancel, Scheduler, TransportFactory, WsTransport } from "./ports";

export type ConnState = "idle" | "connecting" | "handshaking" | "live" | "backoff" | "closed";

/** Session material needed to open + resume a connection. */
export interface SessionProvider {
  accessJwt(): string;
  deviceId(): string;
  resumeToken(): string | undefined;
  setResumeToken(token: string): void;
  cursors(): ConversationCursor[];
}

/** Application callbacks the connection drives. */
export interface WsClientHandlers {
  /** Persist a batch durably, returning the cumulative watermark to ClientAck. */
  onInboxBatch(batch: InboxBatch): Promise<ConversationCursor[]>;
  /** A sent message was accepted — the "sent" tick. */
  onMsgAck(ack: MsgAck): void;
  /** Pending outbox sends to (re)transmit on every live transition. */
  pendingSends(): Promise<MsgSend[]>;
  /** A peer's delivered/read watermark relayed by the server (drives ✓✓ / read). */
  onReceipt?(r: Receipt): void;
  /** A tracked peer's typing/recording indicator. */
  onTyping?(t: Typing): void;
  /** A tracked peer's online/last-seen change. */
  onPresence?(p: PresenceUpdate): void;
  /** An incoming call offer relayed from the caller (dev.{id}.call). */
  onCallOffer?(o: CallOffer): void;
  /** A ring-state transition for a call this device is party to. */
  onCallRing?(r: CallRing): void;
  /** A terminal call end (room finished / peer ended). */
  onCallEnd?(e: CallEnd): void;
  /** A new post in a followed channel — the client pulls it over REST. */
  onChannelEvent?(e: ChannelEvent): void;
  /** 4401 / AUTH_TOKEN_EXPIRED — refresh via REST; the client reconnects. */
  onAuthExpired(): void;
  /** 4403 — device revoked / account suspended: wipe session, stop for good. */
  onRevoked(): void;
  onLive?(ack: HelloAck): void;
  onStateChange?(state: ConnState): void;
}

export interface WsClientOptions {
  transportFactory: TransportFactory;
  scheduler: Scheduler;
  session: SessionProvider;
  handlers: WsClientHandlers;
  rng?: () => number;
  heartbeatMs?: number;
  pongTimeoutMs?: number;
  backoff?: BackoffConfig;
}

export class WsClient {
  private state: ConnState = "idle";
  private transport: WsTransport | null = null;
  private attempt = 0;

  private cancelReconnect: Cancel | null = null;
  private cancelHeartbeat: Cancel | null = null;
  private cancelPongTimeout: Cancel | null = null;

  private readonly rng: () => number;
  private readonly heartbeatMs: number;
  private readonly pongTimeoutMs: number;
  private readonly backoff: BackoffConfig;

  constructor(private readonly o: WsClientOptions) {
    this.rng = o.rng ?? Math.random;
    this.heartbeatMs = o.heartbeatMs ?? 30_000;
    this.pongTimeoutMs = o.pongTimeoutMs ?? 10_000;
    this.backoff = o.backoff ?? defaultBackoff;
  }

  /** Current connection state (for UI banners and tests). */
  get connState(): ConnState {
    return this.state;
  }

  /** start opens the connection; a no-op while already connecting/live. */
  start(): void {
    if (this.state === "idle" || this.state === "closed") this.connect();
  }

  /** stop closes for good — cancels all timers, no reconnect. */
  stop(): void {
    this.clearTimers();
    this.setState("closed");
    this.teardownTransport(CloseCode.Normal);
  }

  /** send transmits a frame iff live; otherwise it is dropped and the durable
   *  outbox replays it on the next live transition (see flush). */
  send(frame: ClientFrame): boolean {
    if (this.state === "live" && this.transport) {
      this.transport.send(frame);
      return true;
    }
    return false;
  }

  /** flush retransmits every pending outbox send (call after enqueue). */
  async flush(): Promise<void> {
    if (this.state !== "live" || !this.transport) return;
    for (const s of await this.o.handlers.pendingSends()) {
      this.transport.send(s);
    }
  }

  // ── internals ──────────────────────────────────────────────────────────────

  private setState(s: ConnState): void {
    if (this.state === s) return;
    this.state = s;
    this.o.handlers.onStateChange?.(s);
  }

  private connect(): void {
    this.cancelReconnect?.();
    this.cancelReconnect = null;
    this.setState("connecting");

    const t = this.o.transportFactory();
    this.transport = t;
    t.onOpen = () => this.onOpen();
    t.onFrame = (f) => {
      void this.onFrame(f);
    };
    t.onClose = (code) => this.onClose(code);
    t.onError = (err) => this.onError(err);
  }

  private onOpen(): void {
    if (!this.transport) return;
    this.setState("handshaking");
    this.transport.send({
      t: "hello",
      accessJwt: this.o.session.accessJwt(),
      deviceId: this.o.session.deviceId(),
      resumeToken: this.o.session.resumeToken(),
      cursors: this.o.session.cursors(),
    });
  }

  private async onFrame(f: ServerFrame): Promise<void> {
    switch (f.t) {
      case "hello_ack":
        await this.onHelloAck(f);
        break;
      case "ping":
        this.transport?.send({ t: "pong" });
        break;
      case "pong":
        this.cancelPongTimeout?.();
        this.cancelPongTimeout = null;
        break;
      case "msg_ack":
        this.o.handlers.onMsgAck(f);
        break;
      case "inbox_batch":
        await this.onInboxBatch(f);
        break;
      case "receipt":
        this.o.handlers.onReceipt?.(f);
        break;
      case "typing":
        this.o.handlers.onTyping?.(f);
        break;
      case "presence_update":
        this.o.handlers.onPresence?.(f);
        break;
      case "call_offer":
        this.o.handlers.onCallOffer?.(f);
        break;
      case "call_ring":
        this.o.handlers.onCallRing?.(f);
        break;
      case "call_end":
        this.o.handlers.onCallEnd?.(f);
        break;
      case "channel_event":
        this.o.handlers.onChannelEvent?.(f);
        break;
      case "server_hint":
        this.onServerHint(f);
        break;
      case "error":
        this.onServerError(f);
        break;
    }
  }

  private async onHelloAck(ack: HelloAck): Promise<void> {
    this.o.session.setResumeToken(ack.resumeToken);
    this.attempt = 0; // a good handshake resets backoff
    this.setState("live");
    this.startHeartbeat();
    this.o.handlers.onLive?.(ack);
    await this.flush(); // retransmit unACKed sends (dedup-safe)
  }

  private async onInboxBatch(batch: InboxBatch): Promise<void> {
    const watermark = await this.o.handlers.onInboxBatch(batch);
    // ACK-after-persist: only now advance the server's delete watermark.
    this.transport?.send({ t: "client_ack", upTo: watermark });
  }

  private onServerHint(hint: ServerHint): void {
    if (hint.kind === "DRAIN") {
      const jitter = Math.round(this.rng() * 1_000);
      this.teardownTransport(CloseCode.ServerDrain);
      this.scheduleReconnect(hint.reconnectAfterMs + jitter);
    }
  }

  private onServerError(err: ServerError): void {
    if (err.code === ErrorCode.AuthTokenExpired) {
      this.o.handlers.onAuthExpired();
      this.teardownTransport(CloseCode.AuthExpired);
      this.scheduleReconnect(this.nextBackoff());
    }
    // Other application errors on a live socket are non-fatal; fatal conditions
    // arrive as close codes and are handled in onClose.
  }

  private onClose(code: number): void {
    this.clearHeartbeat();
    this.transport = null;
    if (this.state === "closed") return;

    switch (code) {
      case CloseCode.Superseded: // 4409 — another connection won; stay closed.
        this.setState("closed");
        return;
      case CloseCode.DeviceRevoked: // 4403 — wipe; do not reconnect.
        this.o.handlers.onRevoked();
        this.setState("closed");
        return;
      case CloseCode.AuthExpired: // 4401 — refresh then reconnect.
        this.o.handlers.onAuthExpired();
        this.scheduleReconnect(this.nextBackoff());
        return;
      default: // 4429 / 1012 / abnormal — backoff + reconnect.
        this.scheduleReconnect(this.nextBackoff());
        return;
    }
  }

  private onError(_err: unknown): void {
    // A well-behaved socket follows an error with a close, but drive teardown
    // ourselves in case it does not.
    if (this.state === "closed") return;
    this.teardownTransport(CloseCode.Normal);
    this.scheduleReconnect(this.nextBackoff());
  }

  private scheduleReconnect(delayMs: number): void {
    this.clearHeartbeat();
    this.setState("backoff");
    this.cancelReconnect?.();
    this.cancelReconnect = this.o.scheduler.setTimeout(() => this.connect(), delayMs);
  }

  private nextBackoff(): number {
    return backoffDelay(this.attempt++, this.backoff, this.rng);
  }

  private startHeartbeat(): void {
    this.clearHeartbeat();
    const tick = (): void => {
      if (this.state !== "live" || !this.transport) return;
      this.transport.send({ t: "ping" });
      this.cancelPongTimeout = this.o.scheduler.setTimeout(() => {
        // No Pong in time → assume a half-open socket; drop and reconnect.
        this.teardownTransport(CloseCode.Normal);
        this.scheduleReconnect(this.nextBackoff());
      }, this.pongTimeoutMs);
      this.cancelHeartbeat = this.o.scheduler.setTimeout(tick, this.heartbeatMs);
    };
    this.cancelHeartbeat = this.o.scheduler.setTimeout(tick, this.heartbeatMs);
  }

  private clearHeartbeat(): void {
    this.cancelHeartbeat?.();
    this.cancelHeartbeat = null;
    this.cancelPongTimeout?.();
    this.cancelPongTimeout = null;
  }

  private clearTimers(): void {
    this.cancelReconnect?.();
    this.cancelReconnect = null;
    this.clearHeartbeat();
  }

  private teardownTransport(code: number): void {
    const t = this.transport;
    this.transport = null;
    if (!t) return;
    // Detach callbacks first so the socket's own close does not re-enter us.
    t.onOpen = null;
    t.onFrame = null;
    t.onClose = null;
    t.onError = null;
    try {
      t.close(code);
    } catch {
      // best-effort
    }
  }
}
