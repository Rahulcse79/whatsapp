// Local TypeScript mirror of the WebSocket frame contract
// (Docs/04-api/websocket-protocol.md). The wire is binary protobuf, but
// @wa/proto-types ships no usable types in the clients CI job (its generated
// src/gen is gitignored and only regenerated in the Go/migrations jobs), so —
// exactly as @wa/sync-engine does — the client defines its own frame shapes
// and the transport owns encode/decode. These are decoded frames: the core
// state machine never touches bytes.

export type UUID = string;

/** MsgKind mirrors the wire enum (websocket-protocol.md §3). */
export enum MsgKind {
  TEXT = 1,
  MEDIA = 2,
  OVERLAY_EDIT = 3,
  OVERLAY_DELETE = 4,
  REACTION = 5,
  PIN = 6,
}

export type ReceiptKind = "DELIVERED" | "READ";
export type ServerHintKind = "DRAIN";

/** A per-conversation replay watermark (Hello.last_cursors / ClientAck.up_to). */
export interface ConversationCursor {
  conversationId: string;
  lastSeq: number;
}

// ── client → server ────────────────────────────────────────────────────────

export interface Hello {
  t: "hello";
  accessJwt: string;
  deviceId: string;
  resumeToken?: string;
  cursors: ConversationCursor[];
}
export interface Ping {
  t: "ping";
}
export interface Pong {
  t: "pong";
}
export interface MsgSend {
  t: "msg_send";
  clientRef: UUID; // frame idempotency key
  msgUuid: UUID; // message idempotency key (equal to clientRef in this shell)
  conversationId: string;
  kind: MsgKind;
  sealedEnvelope: Uint8Array; // libsignal ciphertext; server-opaque
  overlayTarget?: string; // original msg_uuid for overlay kinds
}
export interface ClientAck {
  t: "client_ack";
  upTo: ConversationCursor[]; // cumulative; server deletes ACKed inbox rows
}
export interface Receipt {
  t: "receipt";
  conversationId: string;
  kind: ReceiptKind;
  upToSeq: number;
  fromUserId?: string; // set on the server→client direction (who acked/read)
}
export interface SyncPull {
  t: "sync_pull";
  conversationId: string;
  fromSeq: number;
}
/** Typing is bidirectional: sent while composing (conversationId + recording),
 *  relayed back with the typer's userId. */
export interface Typing {
  t: "typing";
  conversationId: string;
  recording: boolean;
  userId?: string; // set on the server→client direction
}
/** PresenceSub starts/stops tracking users' online-state + typing (client→server). */
export interface PresenceSub {
  t: "presence_sub";
  subscribe: string[];
  unsubscribe: string[];
}

export type ClientFrame = Hello | Ping | Pong | MsgSend | ClientAck | Receipt | SyncPull | Typing | PresenceSub;

// ── server → client ────────────────────────────────────────────────────────

export interface HelloAck {
  t: "hello_ack";
  resumeToken: string;
  sessionId: string;
  serverTimeMs: number;
  replayed: boolean;
}
export interface ServerHint {
  t: "server_hint";
  kind: ServerHintKind;
  reconnectAfterMs: number;
}
export interface ServerError {
  t: "error";
  code: string;
  message?: string;
}
export interface MsgAck {
  t: "msg_ack";
  clientRef: UUID;
  msgUuid: UUID;
  seq: number;
  serverTimeMs: number;
}
export interface InboxItemFrame {
  conversationId: string;
  seq: number;
  msgUuid: UUID;
  senderUserId: string;
  senderDeviceId: string;
  kind: MsgKind;
  overlayTarget?: string;
  ciphertext: Uint8Array;
  acceptedAtMs: number;
}
export interface InboxBatch {
  t: "inbox_batch";
  items: InboxItemFrame[];
}

/** PresenceUpdate relays a tracked user's online/last-seen change (server→client). */
export interface PresenceUpdate {
  t: "presence_update";
  userId: string;
  online: boolean;
  lastSeenMs: number;
}

// ── calls (signaling only — media rides LiveKit, never the WS) ────────────────

/** CallKind mirrors wsv1.CallKind. */
export type CallKind = "voice" | "video";

/** RingState mirrors wsv1.RingState — the 1:1/group call lifecycle relayed to
 *  each party's device (matches @wa/call-engine RingSignal). */
export type RingState = "ringing" | "answered" | "answered_elsewhere" | "declined" | "busy" | "missed" | "ended";

/** CallOffer surfaces an incoming call to the callee's device (server→client,
 *  relayed from the caller on dev.{id}.call). */
export interface CallOffer {
  t: "call_offer";
  ringId: string;
  roomId: string;
  kind: CallKind;
  callerUserId: string;
  callerDeviceId: string;
  participantUserIds: string[];
}

/** CallRing is a ring-state transition for a call this device is party to. */
export interface CallRing {
  t: "call_ring";
  ringId: string;
  state: RingState;
  byUserId: string;
}

/** CallEnd is a terminal call end (e.g. the LiveKit room finished). */
export interface CallEnd {
  t: "call_end";
  ringId: string;
  roomId: string;
  reason: string;
}

// Receipt and Typing are bidirectional: a client sends its watermark / typing
// state, and the server relays the peer's back.
export type ServerFrame =
  | HelloAck
  | Ping
  | Pong
  | ServerHint
  | ServerError
  | MsgAck
  | InboxBatch
  | Receipt
  | Typing
  | PresenceUpdate
  | CallOffer
  | CallRing
  | CallEnd;

/** WSS close codes and their client contract (websocket-protocol.md §6). */
export const CloseCode = {
  Normal: 1000,
  ServerDrain: 1012,
  AuthExpired: 4401,
  DeviceRevoked: 4403,
  Superseded: 4409,
  RateAbuse: 4429,
} as const;

/** Server error codes the connection reacts to. */
export const ErrorCode = {
  AuthTokenExpired: "AUTH_TOKEN_EXPIRED",
} as const;
