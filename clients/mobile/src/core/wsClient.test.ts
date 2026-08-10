import { beforeEach, describe, expect, it } from "vitest";
import {
  MsgKind,
  type ClientFrame,
  type ConversationCursor,
  type InboxBatch,
  type MsgAck,
  type MsgSend,
  type ServerFrame,
} from "./frames";
import type { Cancel, Scheduler, WsTransport } from "./ports";
import { WsClient, type SessionProvider, type WsClientHandlers } from "./wsClient";

// ── deterministic time ───────────────────────────────────────────────────────
class ManualScheduler implements Scheduler {
  time = 0;
  private seq = 0;
  private tasks = new Map<number, { at: number; fn: () => void }>();
  now(): number {
    return this.time;
  }
  setTimeout(fn: () => void, ms: number): Cancel {
    const id = this.seq++;
    this.tasks.set(id, { at: this.time + ms, fn });
    return () => {
      this.tasks.delete(id);
    };
  }
  /** advance runs every task due within [now, now+ms] in chronological order. */
  advance(ms: number): void {
    const target = this.time + ms;
    for (;;) {
      let nextId = -1;
      let nextAt = Infinity;
      for (const [id, t] of this.tasks) {
        if (t.at <= target && t.at < nextAt) {
          nextAt = t.at;
          nextId = id;
        }
      }
      if (nextId === -1) break;
      const t = this.tasks.get(nextId);
      this.tasks.delete(nextId);
      if (t) {
        this.time = t.at;
        t.fn();
      }
    }
    this.time = target;
  }
}

// ── fake socket ──────────────────────────────────────────────────────────────
class FakeTransport implements WsTransport {
  onOpen: (() => void) | null = null;
  onFrame: ((f: ServerFrame) => void) | null = null;
  onClose: ((code: number) => void) | null = null;
  onError: ((err: unknown) => void) | null = null;
  readonly sent: ClientFrame[] = [];
  closedWith: number | null = null;

  send(frame: ClientFrame): void {
    this.sent.push(frame);
  }
  close(code?: number): void {
    this.closedWith = code ?? 1000;
  }
  // test drivers
  open(): void {
    this.onOpen?.();
  }
  emit(f: ServerFrame): void {
    this.onFrame?.(f);
  }
  serverClose(code: number): void {
    this.onClose?.(code);
  }
  sentKinds(): string[] {
    return this.sent.map((f) => f.t);
  }
}

class FakeSession implements SessionProvider {
  jwt = "jwt-1";
  dev = "dev-1";
  resume: string | undefined;
  cur: ConversationCursor[] = [];
  accessJwt(): string {
    return this.jwt;
  }
  deviceId(): string {
    return this.dev;
  }
  resumeToken(): string | undefined {
    return this.resume;
  }
  setResumeToken(t: string): void {
    this.resume = t;
  }
  cursors(): ConversationCursor[] {
    return this.cur;
  }
}

const helloAck = (resume = "r-new"): ServerFrame => ({
  t: "hello_ack",
  resumeToken: resume,
  sessionId: "s1",
  serverTimeMs: 1,
  replayed: false,
});

const settle = async (): Promise<void> => {
  for (let i = 0; i < 8; i++) await Promise.resolve();
};

interface Harness {
  client: WsClient;
  scheduler: ManualScheduler;
  transports: FakeTransport[];
  session: FakeSession;
  inbox: InboxBatch[];
  acks: MsgAck[];
  pending: MsgSend[];
  events: { authExpired: number; revoked: number };
}

function setup(pending: MsgSend[] = []): Harness {
  const scheduler = new ManualScheduler();
  const transports: FakeTransport[] = [];
  const session = new FakeSession();
  const inbox: InboxBatch[] = [];
  const acks: MsgAck[] = [];
  const events = { authExpired: 0, revoked: 0 };

  const handlers: WsClientHandlers = {
    onInboxBatch: (b) => {
      inbox.push(b);
      return Promise.resolve(b.items.map((i) => ({ conversationId: i.conversationId, lastSeq: i.seq })));
    },
    onMsgAck: (a) => acks.push(a),
    pendingSends: () => Promise.resolve(pending),
    onAuthExpired: () => {
      events.authExpired++;
    },
    onRevoked: () => {
      events.revoked++;
    },
  };

  const client = new WsClient({
    transportFactory: () => {
      const t = new FakeTransport();
      transports.push(t);
      return t;
    },
    scheduler,
    session,
    handlers,
    rng: () => 0.5,
    heartbeatMs: 30_000,
    pongTimeoutMs: 10_000,
  });

  return { client, scheduler, transports, session, inbox, acks, pending, events };
}

const last = <T>(a: T[]): T => a[a.length - 1] as T;

describe("WsClient handshake + resume", () => {
  let h: Harness;
  beforeEach(() => {
    h = setup();
  });

  it("sends Hello with cursors + resume token on open, goes live on HelloAck", async () => {
    h.session.resume = "r0";
    h.session.cur = [{ conversationId: "c1", lastSeq: 7 }];
    h.client.start();
    expect(h.client.connState).toBe("connecting");

    last(h.transports).open();
    expect(h.client.connState).toBe("handshaking");
    const hello = last(h.transports).sent[0];
    expect(hello).toMatchObject({ t: "hello", resumeToken: "r0", cursors: [{ conversationId: "c1", lastSeq: 7 }] });

    last(h.transports).emit(helloAck("r1"));
    await settle();
    expect(h.client.connState).toBe("live");
    expect(h.session.resume).toBe("r1"); // resume token stored for next reconnect
  });

  it("persists an inbox batch, THEN sends ClientAck at the watermark", async () => {
    h.client.start();
    last(h.transports).open();
    last(h.transports).emit(helloAck());
    await settle();

    const batch: InboxBatch = {
      t: "inbox_batch",
      items: [
        {
          conversationId: "c1",
          seq: 9,
          msgUuid: "m9",
          senderUserId: "u2",
          senderDeviceId: "d2",
          kind: MsgKind.TEXT,
          ciphertext: new Uint8Array([1]),
          acceptedAtMs: 100,
        },
      ],
    };
    last(h.transports).emit(batch);
    await settle();

    expect(h.inbox).toHaveLength(1);
    const ack = last(h.transports).sent.find((f) => f.t === "client_ack");
    expect(ack).toEqual({ t: "client_ack", upTo: [{ conversationId: "c1", lastSeq: 9 }] });
  });

  it("routes MsgAck to the handler", async () => {
    h.client.start();
    last(h.transports).open();
    last(h.transports).emit(helloAck());
    await settle();

    last(h.transports).emit({ t: "msg_ack", clientRef: "cr1", msgUuid: "cr1", seq: 3, serverTimeMs: 5 });
    expect(h.acks).toEqual([{ t: "msg_ack", clientRef: "cr1", msgUuid: "cr1", seq: 3, serverTimeMs: 5 }]);
  });
});

describe("WsClient heartbeat", () => {
  it("pings on the interval and reconnects when no Pong arrives", async () => {
    const h = setup();
    h.client.start();
    last(h.transports).open();
    last(h.transports).emit(helloAck());
    await settle();

    h.scheduler.advance(30_000); // heartbeat tick
    expect(last(h.transports).sentKinds()).toContain("ping");

    h.scheduler.advance(10_000); // pong never came → drop
    expect(last(h.transports).closedWith).not.toBeNull();
    expect(h.client.connState).toBe("backoff");

    h.scheduler.advance(60_000); // backoff elapses → fresh connection
    expect(h.transports).toHaveLength(2);
    expect(h.client.connState).toBe("connecting");
  });

  it("a Pong keeps the connection alive", async () => {
    const h = setup();
    h.client.start();
    last(h.transports).open();
    last(h.transports).emit(helloAck());
    await settle();

    h.scheduler.advance(30_000); // ping
    last(h.transports).emit({ t: "pong" });
    h.scheduler.advance(10_000); // pong-timeout window passes harmlessly
    expect(h.client.connState).toBe("live");
    expect(h.transports).toHaveLength(1);
  });
});

describe("WsClient close-code contract", () => {
  it("4409 (superseded) stays closed — no reconnect", () => {
    const h = setup();
    h.client.start();
    last(h.transports).open();
    last(h.transports).serverClose(4409);
    expect(h.client.connState).toBe("closed");
    h.scheduler.advance(120_000);
    expect(h.transports).toHaveLength(1);
  });

  it("4403 (revoked) wipes and stays closed", () => {
    const h = setup();
    h.client.start();
    last(h.transports).open();
    last(h.transports).serverClose(4403);
    expect(h.events.revoked).toBe(1);
    expect(h.client.connState).toBe("closed");
    h.scheduler.advance(120_000);
    expect(h.transports).toHaveLength(1);
  });

  it("4401 (auth expired) refreshes and reconnects", () => {
    const h = setup();
    h.client.start();
    last(h.transports).open();
    last(h.transports).serverClose(4401);
    expect(h.events.authExpired).toBe(1);
    h.scheduler.advance(60_000);
    expect(h.transports).toHaveLength(2); // reconnected with (soon-to-be-refreshed) token
  });

  it("an abnormal close backs off and reconnects", () => {
    const h = setup();
    h.client.start();
    last(h.transports).open();
    last(h.transports).emit(helloAck());
    last(h.transports).serverClose(1006);
    expect(h.client.connState).toBe("backoff");
    h.scheduler.advance(60_000);
    expect(h.transports).toHaveLength(2);
  });

  it("DRAIN hint reconnects after the hinted delay", () => {
    const h = setup();
    h.client.start();
    last(h.transports).open();
    last(h.transports).emit(helloAck());
    last(h.transports).emit({ t: "server_hint", kind: "DRAIN", reconnectAfterMs: 5_000 });
    expect(h.client.connState).toBe("backoff");
    h.scheduler.advance(5_400); // 5000 + jitter(rng 0.5 → 500)
    expect(h.transports).toHaveLength(1); // not yet
    h.scheduler.advance(200);
    expect(h.transports).toHaveLength(2);
  });
});

describe("WsClient outbox resend across reconnect", () => {
  it("retransmits pending sends on every live transition (dedup-safe)", async () => {
    const send: MsgSend = {
      t: "msg_send",
      clientRef: "cr1",
      msgUuid: "cr1",
      conversationId: "c1",
      kind: MsgKind.TEXT,
      sealedEnvelope: new Uint8Array([9]),
    };
    const h = setup([send]);

    h.client.start();
    last(h.transports).open();
    last(h.transports).emit(helloAck());
    await settle();
    expect(h.transports[0]?.sent.filter((f) => f.t === "msg_send")).toHaveLength(1);

    last(h.transports).serverClose(1006); // drop
    h.scheduler.advance(60_000); // reconnect
    expect(h.transports).toHaveLength(2);

    last(h.transports).open();
    last(h.transports).emit(helloAck());
    await settle();
    // Resent on the new connection — the server dedupes by msg_uuid.
    expect(h.transports[1]?.sent.filter((f) => f.t === "msg_send")).toHaveLength(1);
  });
});
