# WebSocket Protocol — Protobuf Frame Specification

| Doc | The realtime protocol: framing, handshake, every frame type |
|---|---|
| Status | v1.0 |
| Wire | WSS, binary protobuf; one multiplexed connection per device; schemas live in `server/proto/` (source of truth, codegen Go+TS) |

## 1. Connection lifecycle

```
connect WSS /v1/ws
  → client: Hello{access_jwt, device_id, resume_token?, last_cursors?}
  → server: HelloAck{resume_token, session_id, server_time, replayed: bool}
     [invalid/expired JWT → Error{AUTH_TOKEN_EXPIRED} + close 4401 → client refreshes via REST]
  → if resume: server replays inbox WHERE seq > cursor (batched InboxBatch frames)
  → live traffic; heartbeat Ping/Pong every 30 s ± jitter
  → deploy drain: server sends ServerHint{DRAIN, reconnect_after_ms} → client reconnects gracefully
```

Client → server frames carry `client_ref` (UUIDv7) — every one is ACKed or errored; the outbox resends unACKed frames on reconnect (server dedupes).

## 2. Envelope

```protobuf
message Frame {
  uint64 frame_id   = 1;   // per-connection monotonic (debug/tracing)
  oneof body {
    // session
    Hello hello = 10;  HelloAck hello_ack = 11;  Ping ping = 12;  Pong pong = 13;
    ServerHint server_hint = 14;  Error error = 15;
    // messaging
    MsgSend msg_send = 20;  MsgAck msg_ack = 21;  InboxBatch inbox_batch = 22;
    ClientAck client_ack = 23;  Receipt receipt = 24;  Overlay overlay = 25;
    SyncPull sync_pull = 26;
    // presence
    PresenceSub presence_sub = 30;  PresenceUpdate presence_update = 31;  Typing typing = 32;
    // groups
    GroupEvent group_event = 40;
    // calls
    CallOffer call_offer = 50;  CallAnswer call_answer = 51;  CallDecline call_decline = 52;
    CallEnd call_end = 53;  CallRing call_ring = 54;
    // ptt
    PttRequest ptt_request = 60;  PttGrant ptt_grant = 61;  PttRelease ptt_release = 62;
    PttQueuePos ptt_queue_pos = 63;
  }
}
```

## 3. Key frames (semantics)

### MsgSend (client → server)
```protobuf
message MsgSend {
  string msg_uuid = 1;          // UUIDv7, idempotency key
  string conversation_id = 2;
  bytes  sealed_envelope = 3;   // libsignal ciphertext (per-device or SenderKey), incl. enc thumbnail for media
  MsgKind kind = 4;             // TEXT|MEDIA|OVERLAY_EDIT|OVERLAY_DELETE|REACTION|PIN|STORY_KEY…
  string overlay_target = 5;    // for overlays: original msg_uuid
}
```
Server validates: sender authz, conversation membership, rate limits, overlay windows (edit ≤ 15 min, delete ≤ 48 h — by server-tracked accept time, since content is invisible). Response: `MsgAck{msg_uuid, seq, server_time}` = the "sent" tick.

### InboxBatch (server → client) / ClientAck (client → server)
Delivery unit: ordered `[ {conversation_id, seq, msg_uuid, sender, ciphertext} ]`. Client persists locally **then** sends `ClientAck{up_to: [(conversation_id, seq)]}` — cumulative watermark; server deletes ACKed inbox rows. **ACK-after-persist is the client-side durability contract.**

### Receipt
`{conversation_id, kind: DELIVERED|READ, up_to_seq}` — cumulative; coalesced ≤ 1/250 ms per conversation both directions; READ suppressed server-side if recipient's privacy disables read receipts.

### PresenceSub / PresenceUpdate / Typing
Subscription-based presence (DS&A doc §10): `PresenceSub{subscribe: [user_id], unsubscribe: […]}` capped at 50 concurrent subs/device. `Typing{conversation_id}` throttle 1/3 s client-side, TTL 5 s server-side.

### Call* frames
Signaling only — media never crosses the WS. `CallOffer{room_id, call_kind, join_token, caller, participants}`; ring state machine server-side (45 s → missed). ICE/SDP negotiation happens client↔LiveKit; `call.ice-hint` reserved for TURN config push.

### PttRequest/Grant
`PttRequest{room_id, action: ACQUIRE|RELEASE|HEARTBEAT}` → `PttGrant{fence, max_speak_ms}` or `PttQueuePos{position}`. Client unmutes its pre-negotiated track only while holding an unexpired grant; fence travels to the SFU permission update (rtc-lld.md).

## 4. Ordering & delivery guarantees (the contract clients build on)

1. Within a conversation, frames arrive in `seq` order per connection; gaps → client issues `SyncPull{conversation_id, from_seq}`.
2. Every `MsgSend` gets exactly one `MsgAck` or `Error` (possibly after reconnect+resend — dedupe makes the retry invisible).
3. A message ACKed by the server (`MsgAck`) will reach every recipient device that stays registered, within 30 days — or die trying (inbox durability, NFR-12).
4. Duplicates may arrive on the wire (at-least-once); clients dedupe by `msg_uuid`. Effective UX: exactly-once.
5. Receipts/presence/typing are lossy-by-design conveniences — never load-bearing.

## 5. Flow control & limits

- Frame size cap 256 KB (media goes via presigned upload, never through WS).
- Server send-buffer per connection 1 MB high-water; slow consumers get `InboxBatch` pacing (inbox holds the backlog — memory does not).
- App-level zstd for frames > 1 KB (negotiated in Hello).
- Per-device inflight-unACKed cap: 512 frames → backpressure to NATS consumer (pull-based).

## 6. Close codes

| Code | Meaning | Client action |
|---|---|---|
| 4401 | auth expired/invalid | REST refresh → reconnect |
| 4403 | device revoked / account suspended | wipe session, re-register flow |
| 4409 | newer connection for this device claimed the route | stay closed (other tab/device won) |
| 4429 | connection-level rate abuse | backoff per `retry_after` |
| 1012 | server drain (deploy) | reconnect after hint delay with jitter |
