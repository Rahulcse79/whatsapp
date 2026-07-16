# Internal Events — NATS JetStream Subjects & Contracts

| Doc | Streams, subjects, consumers, delivery contracts |
|---|---|
| Status | v1.0 |
| Invariant | NATS is **transit**; PG inbox is truth. Any stream can be wiped and the system converges (delivery stalls heal by inbox replay). Payloads: protobuf from `server/proto/events/`. |

## 1. Streams & subjects

| Stream | Subjects | Retention | Purpose |
|---|---|---|---|
| `DELIVERY` | `dev.{device_id}.out` | 24 h / 1M msgs per subject cap | Live delivery to the gateway holding the device |
| `INGRESS` | `msg.in` | 1 h | gateway → chat accept handoff (burst buffer) |
| `PUSH` | `push.dispatch` | 24 h | notification-svc work queue |
| `DOMAIN` | `group.events.{group_id}`, `call.events`, `media.lifecycle`, `user.events` | 24 h | cross-context reactions (key rotation, GC, analytics rollups) |
| `ANALYTICS` | `analytics.counters` | 7 d | metadata-only rollup feed (HLD §18.1) |

## 2. Consumer contracts

| Consumer | Stream | Mode | Ack policy |
|---|---|---|---|
| ws-gateway (per pod) | `DELIVERY` filtered to routed devices | push, flow-controlled | ack after WS write; redelivery on no-ack 30 s |
| chat accept workers | `INGRESS` | pull batch ≤ 100 | ack after inbox INSERT commits |
| notification-svc | `PUSH` | pull, durable | ack after provider accept OR breaker-queue persist; ≤ 5 redeliveries → `push.dlq` + alert |
| fan-out worker | `DOMAIN group.events` | durable | ack after membership cache bump |
| analytics aggregator | `ANALYTICS` | pull batch | at-least-once into idempotent daily rollups |

## 3. Delivery semantics

- **At-least-once everywhere**; consumers idempotent (msg_uuid / event_id keys).
- Per-subject ordering only (`dev.{id}.out` is ordered per device — sufficient: cross-device ordering is meaningless; conversation order is by `seq`, not arrival).
- Redelivery ≤ 5 → DLQ subject + page. DLQ inspection is a runbook, not a cron.

## 4. Event catalog (payload sketch)

| Event | Emitted when | Key fields | Consumers |
|---|---|---|---|
| `msg.accepted` → `dev.*.out` | inbox row committed | conv, seq, msg_uuid, ciphertext ref | gateways |
| `push.dispatch` | recipient has no live route | device, kind(msg/call/voip), collapse_key | notification-svc |
| `group.member_added/removed/role_changed` | membership tx commits | group, version, actor, subject | fan-out cache, clients (as group_event), key-rotation trigger |
| `call.room_created/ended` | call-ctl / LiveKit webhook | room, participants, outcome | call history, analytics |
| `media.uploaded/orphaned` | media-svc | object_key, refcount | GC job |
| `user.suspended/deleted` | admin action / self-delete | user, actor | session killer, purge job |

## 5. Subject ACLs (least privilege, per deployable)

| Deployable | Publish | Subscribe |
|---|---|---|
| core-api | `dev.*.out`, `push.dispatch`, `group.events.*`, `call.events`, `user.events`, `analytics.*` | `msg.in`, `media.lifecycle` |
| ws-gateway | `msg.in` | `dev.*.out` (filtered) |
| media-svc | `media.lifecycle` | `media.lifecycle` |
| notification-svc | — | `push.dispatch` |
