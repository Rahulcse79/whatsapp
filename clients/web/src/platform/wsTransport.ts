import { create, fromBinary, toBinary } from "@bufbuild/protobuf";
import { FrameSchema, ReceiptKind as PbReceiptKind } from "@wa/proto-types";
import {
  MsgKind,
  type ClientFrame,
  type ConversationCursor,
  type InboxItemFrame,
  type ServerFrame,
  type TransportFactory,
  type WsTransport,
} from "@wa/client-core";

// Binary protobuf frame codec (websocket-protocol.md §Wire). The gateway speaks
// wsv1.Frame envelopes as BINARY WebSocket messages; this maps the client-core
// frame shapes ↔ the generated protobuf-es types. (Replaces the earlier JSON
// dev codec, which the protobuf-only gateway rejected on the handshake with a
// 4400 "expected hello frame" close — that mismatch was why messages never
// delivered.)
export function createWsTransportFactory(url: string): TransportFactory {
  return () => new ProtoWsTransport(url);
}

class ProtoWsTransport implements WsTransport {
  onOpen: (() => void) | null = null;
  onFrame: ((f: ServerFrame) => void) | null = null;
  onClose: ((code: number) => void) | null = null;
  onError: ((err: unknown) => void) | null = null;

  private readonly ws: WebSocket;

  constructor(url: string) {
    this.ws = new WebSocket(url);
    this.ws.binaryType = "arraybuffer";
    this.ws.onopen = () => this.onOpen?.();
    this.ws.onmessage = (e: MessageEvent) => {
      try {
        const f = decodeServerFrame(new Uint8Array(e.data as ArrayBuffer));
        if (f) this.onFrame?.(f);
      } catch (err) {
        this.onError?.(err);
      }
    };
    this.ws.onclose = (e: CloseEvent) => this.onClose?.(e.code);
    this.ws.onerror = (e) => this.onError?.(e);
  }

  send(frame: ClientFrame): void {
    this.ws.send(encodeClientFrame(frame));
  }
  close(code?: number): void {
    this.ws.close(code);
  }
}

// ── client → server ──────────────────────────────────────────────────────────

function encodeClientFrame(frame: ClientFrame): Uint8Array {
  // clientBody is structurally a valid oneof member but its `case` is widened to
  // string; assert it into the generated discriminated union at this boundary.
  return toBinary(FrameSchema, create(FrameSchema, { body: clientBody(frame) as never }));
}

// The oneof body init; typed loosely so we don't restate the generated union.
type BodyInit = { case: string; value: Record<string, unknown> };

function clientBody(frame: ClientFrame): BodyInit {
  switch (frame.t) {
    case "hello":
      return {
        case: "hello",
        value: {
          accessJwt: frame.accessJwt,
          deviceId: frame.deviceId,
          resumeToken: frame.resumeToken ?? "",
          lastCursors: frame.cursors.map(pbCursor),
        },
      };
    case "ping":
      return { case: "ping", value: { tsMs: 0n } };
    case "pong":
      return { case: "pong", value: { echoTsMs: 0n } };
    case "msg_send":
      return {
        case: "msgSend",
        value: {
          msgUuid: frame.msgUuid,
          conversationId: frame.conversationId,
          sealedEnvelope: frame.sealedEnvelope,
          kind: frame.kind as number,
          overlayTarget: frame.overlayTarget ?? "",
        },
      };
    case "client_ack":
      return { case: "clientAck", value: { upTo: frame.upTo.map(pbCursor) } };
    case "receipt":
      return {
        case: "receipt",
        value: {
          conversationId: frame.conversationId,
          kind: frame.kind === "READ" ? PbReceiptKind.READ : PbReceiptKind.DELIVERED,
          upToSeq: BigInt(frame.upToSeq),
        },
      };
    case "sync_pull":
      return {
        case: "syncPull",
        value: { conversationId: frame.conversationId, fromSeq: BigInt(frame.fromSeq) },
      };
    case "typing":
      return { case: "typing", value: { conversationId: frame.conversationId, recording: frame.recording } };
    case "presence_sub":
      return {
        case: "presenceSub",
        value: { subscribeUserIds: frame.subscribe, unsubscribeUserIds: frame.unsubscribe },
      };
  }
}

function pbCursor(c: ConversationCursor): Record<string, unknown> {
  return { conversationId: c.conversationId, lastSeq: BigInt(c.lastSeq) };
}

// ── server → client ──────────────────────────────────────────────────────────

function decodeServerFrame(data: Uint8Array): ServerFrame | null {
  const b = fromBinary(FrameSchema, data).body;
  switch (b.case) {
    case "helloAck":
      return {
        t: "hello_ack",
        resumeToken: b.value.resumeToken,
        sessionId: b.value.sessionId,
        serverTimeMs: Number(b.value.serverTimeMs),
        replayed: b.value.replayPending,
      };
    case "ping":
      return { t: "ping" };
    case "pong":
      return { t: "pong" };
    case "serverHint":
      return { t: "server_hint", kind: "DRAIN", reconnectAfterMs: Number(b.value.reconnectAfterMs) };
    case "error":
      return { t: "error", code: b.value.code, message: b.value.message };
    case "receipt":
      return {
        t: "receipt",
        conversationId: b.value.conversationId,
        kind: b.value.kind === PbReceiptKind.READ ? "READ" : "DELIVERED",
        upToSeq: Number(b.value.upToSeq),
        fromUserId: b.value.fromUserId || undefined,
      };
    case "typing":
      return {
        t: "typing",
        conversationId: b.value.conversationId,
        recording: b.value.recording,
        userId: b.value.userId || undefined,
      };
    case "presenceUpdate":
      return {
        t: "presence_update",
        userId: b.value.userId,
        online: b.value.online,
        lastSeenMs: Number(b.value.lastSeenMs),
      };
    case "msgAck":
      return {
        t: "msg_ack",
        clientRef: b.value.msgUuid, // clientRef == msgUuid in this shell
        msgUuid: b.value.msgUuid,
        seq: Number(b.value.seq),
        serverTimeMs: Number(b.value.serverTimeMs),
      };
    case "inboxBatch":
      return {
        t: "inbox_batch",
        items: b.value.items.map(
          (it): InboxItemFrame => ({
            conversationId: it.conversationId,
            seq: Number(it.seq),
            msgUuid: it.msgUuid,
            senderUserId: it.senderUserId,
            senderDeviceId: it.senderDeviceId,
            kind: it.kind as unknown as MsgKind,
            overlayTarget: it.overlayTarget || undefined,
            ciphertext: it.sealedEnvelope,
            acceptedAtMs: Number(it.acceptedAtMs),
          }),
        ),
      };
    default:
      return null; // presence/typing/call/ptt frames the shell doesn't handle yet
  }
}
