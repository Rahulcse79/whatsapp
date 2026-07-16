# Microservice Architecture — Bounded Contexts & Deployables

| Doc | Service decomposition |
|---|---|
| Status | v1.0 |
| Upstream | [/HLD.md](../../HLD.md) §4.3, §5 |

## 1. Philosophy: contexts ≠ deployables

We define **9 bounded contexts** with hard internal interfaces, shipped as **5 deployables**. At 20k concurrent, a 20-service constellation multiplies failure modes, network hops, and on-call burden for zero benefit. Contexts are extraction-ready: each has its own package, interface, and DB role — splitting one out is a deployment change, not a rewrite.

## 2. Bounded contexts

| Context | Owns | Primary data | Hot path? |
|---|---|---|---|
| Auth & Identity | OTP, JWT lifecycle, devices, 2FA PIN, sessions | `users`, `devices`, `otp_attempts` | No — rate-limit heavy |
| Keys | Signal prekey distribution (public keys only) | `prekeys`, `signed_prekeys` | Read bursts |
| Chat | Accept/dedupe/sequence, inbox fan-out, receipts, overlays | `message_inbox`, `conversations` | **Yes — the hot path** |
| Groups | Membership, roles, invites, metadata | `groups`, `group_members`, `invite_links` | Fan-out multiplier |
| Media | Upload sessions, presign, quotas, GC | `media_objects` + MinIO | Bandwidth-bound |
| Calls & PTT | Room lifecycle, tokens, ring state, floor control | `call_records` + Valkey floor | Latency-critical |
| Presence | Online/last-seen, typing, subscriptions | Valkey only | High-frequency, ephemeral |
| Notifications | Push tokens, dispatch, provider failover | `push_tokens` | External-bound |
| Admin & T&S | Reports, actions, flags, rollups | `reports`, `audit_log`, `feature_flags` | No |

## 3. Deployables

| Deployable | Contexts | Scaling axis | Why isolated |
|---|---|---|---|
| `core-api` | auth, keys, chat, groups, calls-ctl/PTT, stories, presence logic, admin | CPU (HPA) | Shared data model; one binary = simple ops |
| `ws-gateway` | connection handling, frame routing, resume | **Connection count** | Isolates reconnect storms from business logic |
| `media-svc` | media | Bandwidth | Large-payload traffic away from API pods |
| `notification-svc` | notifications | External API latency | Provider outages must not backpressure chat |
| `rtc` (infra) | LiveKit + coturn | NIC egress | Host-network media plane |

## 4. Communication matrix

| From → To | Mechanism | Contract |
|---|---|---|
| Client → core-api | REST/HTTPS | [04-api/](../04-api/api-standards.md) |
| Client → ws-gateway | WSS protobuf frames | [websocket-protocol.md](../04-api/websocket-protocol.md) |
| ws-gateway → core-api | gRPC (auth check, frame handoff) | proto/ |
| core-api → everyone | NATS JetStream events | [internal-events-nats.md](../04-api/internal-events-nats.md) |
| ws-gateway ← NATS | per-device subjects `dev.{id}.out` | at-least-once, client dedupes |
| media-svc ↔ core-api | gRPC (quota check) — the only sync cross-call | keep it that way |
| LiveKit → core-api | Webhooks (room lifecycle) | HLD §10.2 |
| notification-svc ← NATS | `push.dispatch` durable consumer | retry/backoff owned here |

**Rule:** cross-context communication inside core-api goes through the context's Go interface, never by importing another context's internals — enforced by `internal/` package layout + lint (see [coding-standards.md](../13-standards/coding-standards.md)).

## 5. Split triggers (decided in advance, HLD §4.3)

Extract a context from core-api when **(a)** its load profile diverges ≥ 5× from siblings, **(b)** it needs an independent release cadence, or **(c)** a dedicated team owns it. Predicted first extractions at ~100k concurrent: `chat`, `presence`.

## 6. What each deployable may touch (least privilege)

| Deployable | PG role grants | Valkey prefixes | NATS subjects | MinIO |
|---|---|---|---|---|
| core-api | all app tables | all except `rl:edge:*` | pub `msg.*`, `push.*`, `call.*`, `group.*`; sub internal | — |
| ws-gateway | **none** (via gRPC only) | `route:*`, `presence:*`, `typing:*` | sub `dev.*.out`; pub `msg.in` | — |
| media-svc | `media_objects` only | `rl:media:*` | pub/sub `media.lifecycle` | presign + lifecycle admin |
| notification-svc | `push_tokens` only | `rl:push:*` | sub `push.dispatch` | — |
| rtc | none | none | — (webhooks out) | egress bucket only (org rooms) |
