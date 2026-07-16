# API — Calls & PTT (Control Plane)

| Doc | Call/PTT REST + WS signaling surface |
|---|---|
| Status | v1.0 · Media plane is LiveKit/WebRTC ([rtc-lld.md](../05-services/rtc-lld.md)); this doc is the control plane only |

## Calls REST (`/v1/calls`) — FR-CALL-*

| Endpoint | Req → Resp | Notes |
|---|---|---|
| `POST /` | `{callee_ids[≤31], kind: voice\|video}` → `{room_id, join_token, ring_id}` | Creates LiveKit room, mints caller's short-lived join JWT (≤ 60 s TTL, room-scoped), starts ring state machine, dispatches WS `CallOffer` + VoIP push |
| `POST /{ring_id}/answer` | → `{join_token}` | Ring machine → answered; other callee devices get `CallEnd{ANSWERED_ELSEWHERE}` |
| `POST /{ring_id}/decline` | → 204 | busy/decline propagated to caller |
| `GET /history?cursor=` | → `[{id, kind, participants, direction, outcome, started, duration}]` | Metadata only, 90 d (FR-CALL-06) |
| `POST /{room_id}/rejoin` | → fresh `join_token` | Network-change recovery aid (ICE restart is client↔SFU) |

Ring state machine (server-authoritative): `ringing → answered | declined | busy | missed(45 s)`; every transition emits WS frames to all involved devices + missed-call push/notification.

## PTT (`/v1/ptt` + WS frames) — FR-PTT-*

| Surface | Contract |
|---|---|
| `POST /rooms` | `{name, member_source: group_id\|ad-hoc[]}` → `{room_id, join_token}` — audio-only LiveKit room; all mics pre-negotiated **server-muted** |
| `POST /rooms/{id}/join` | → `{join_token, current_speaker?, queue_len}` — up to 500 listeners |
| WS `PttRequest{ACQUIRE}` | → `PttGrant{fence, max_speak_ms:60000}` or `PttQueuePos{n}` — atomic Lua ([valkey-keyspace.md](../03-database/valkey-keyspace.md) §2) |
| WS `PttRequest{HEARTBEAT}` | every 500 ms while speaking; 2 missed → auto-release → next in FIFO |
| WS `PttRequest{RELEASE}` | explicit release on button-up |
| Enforcement | Grant flips LiveKit publish permission **for that fence only**; stale-fence audio is dead at the SFU (media-plane enforcement, FR-PTT-04) |

Latency budget (must hold at p95 ≤ 200 ms): acquire ~30 ms + permission flip ~40 ms + first RTP on pre-negotiated track ~80–100 ms.

## Push registration (`/v1/push`)

| Endpoint | Notes |
|---|---|
| `PUT /token` | `{provider: fcm\|apns\|apns_voip\|ntfy\|webpush, token}` — per device; provider set constrained by deployment profile (offline → ntfy/webpush) |
| `DELETE /token` | On logout; revocation also clears it server-side |
