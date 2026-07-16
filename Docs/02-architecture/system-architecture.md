# System Architecture

| Doc | System architecture — working summary |
|---|---|
| Status | v1.0 |
| Canonical | [/HLD.md](../../HLD.md) — this doc condenses; the HLD decides. |

## 1. Guiding principles (HLD §4.1)

1. **Local-first clients, dumb-relay server.** E2EE forces the server into a relay role; we embrace it — clients own history, search, previews.
2. **Stateless service tier.** Durable state only in PostgreSQL, Valkey, NATS, MinIO. Any pod may die at any moment.
3. **At-least-once + idempotency = effectively-once.** Client-generated UUIDv7 on every mutation; server dedupes.
4. **One writer of truth per data type.** No dual writes; derived data is rebuildable.
5. **Boring technology, few moving parts.**
6. **Right-size now, leave seams.** Bounded contexts have hard interfaces so extraction is a deploy change, not a rewrite.

## 2. System context

```
        React Native (iOS/Android)          React + Vite PWA (web/desktop)
        SQLCipher · libsignal · outbox      SQLCipher-wasm/OPFS · libsignal
                 │                                   │
     HTTPS(REST) │ WSS(protobuf)          WebRTC(SRTP+SFrame E2EE)
                 ▼                                   ▼
        ┌─────────────────────────┐        ┌──────────────────┐
        │ Envoy Gateway (TLS1.3,  │        │ coturn STUN/TURN │
        │ WAF, rate limits, LB)   │        └────────┬─────────┘
        └───┬────────────┬────────┘                 │
            ▼            ▼                          ▼
      ┌──────────┐ ┌────────────┐          ┌────────────────┐
      │ core-api │ │ ws-gateway │          │ LiveKit SFU ×2 │
      │ (modular │ │ (Go, ~20k  │          │ voice/video/PTT│
      │ monolith)│ │ conns/pod) │          └───────┬────────┘
      └─┬───┬────┘ └─────┬──────┘                  │ webhooks
        │   │            │                         ▼
        │   │      ┌─────▼──────────────┐   call-ctl (in core-api)
        │   │      │ NATS JetStream ×3  │──► notification-svc ─► FCM/APNs/ntfy
        │   │      └─────┬──────────────┘
        ▼   ▼            ▼
  ┌─────────┐ ┌──────────────┐ ┌────────────────┐ ┌──────────────┐
  │media-svc│ │ PostgreSQL 17│ │ Valkey (HA)    │ │ MinIO (EC)   │
  │presign/GC│ │ inbox·meta· │ │ presence·route·│ │ ciphertext   │
  └─────────┘ │ keys         │ │ dedupe·floor   │ │ media/backups│
              └──────────────┘ └────────────────┘ └──────────────┘
```

## 3. The one flow that explains the system

A message send: client encrypts per recipient device (libsignal) → WS frame with UUIDv7 → ws-gateway authenticates and forwards → chat module dedupes (Valkey), assigns per-conversation sequence, writes `message_inbox` rows (durable), publishes to per-device NATS subjects → online devices get it via their gateway; offline devices trigger a data-only push → recipient ACK deletes the inbox row → receipts flow back. **NATS is transit; PostgreSQL inbox is truth.** Reconnect replays from the inbox cursor, so a gateway can die at any step with zero loss after server ACK.

## 4. Where every kind of state lives

| State | Store | Why |
|---|---|---|
| Undelivered ciphertext | PG `message_inbox` (partitioned) | Durability + cursor replay |
| Identity, devices, groups, keys, media refs | PG | Relational, transactional |
| Presence, typing, routes, dedupe, PTT floor, rate limits | Valkey | Ephemeral, TTL-native, atomic ops |
| In-flight events, push dispatch | NATS JetStream | Durable at-least-once transit |
| Media/backup blobs (ciphertext) | MinIO | S3 semantics, presigned direct I/O |
| Chat history, search index, sessions | Client SQLCipher | E2EE: only clients can read content |

## 5. Trust boundaries

1. **Client ↔ edge:** TLS 1.3 + cert pinning; JWT auth; everything past Envoy is authenticated.
2. **Content boundary (the big one):** plaintext exists **only** on user devices. Server-side code operates on ciphertext + minimal metadata by construction.
3. **Cluster-internal:** mTLS; least-privilege DB roles per deployable; NATS subject ACLs per service.
4. **Admin plane:** separate SPA, SSO+2FA, IP allowlist, immutable audit log (HLD §15.6).

## 6. Deviations from common practice — deliberate, documented

| Convention elsewhere | Here | ADR |
|---|---|---|
| Kafka event bus | NATS JetStream | [ADR-003](adr/ADR-003-nats-over-kafka.md) |
| Server message archive | 30-day relay buffer | [ADR-001](adr/ADR-001-relay-model.md) |
| Elasticsearch/OpenSearch | Client FTS + PG trigram | [ADR-005](adr/ADR-005-client-side-search.md) |
| 15–25 microservices | 5 deployables | [microservices.md](microservices.md) |
| Next.js / Flutter | Vite PWA / React Native | [ADR-006](adr/ADR-006-client-stack.md) |
