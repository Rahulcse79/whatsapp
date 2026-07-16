# Performance Optimization

| Doc | Optimization checklist + bottleneck register with mitigations |
|---|---|
| Status | v1.0 · Upstream: [/HLD.md](../../HLD.md) §20–21 · Rule: optimize against a measured SLI, never speculatively |

## 1. Protocol & wire

- Protobuf frames (2–5× smaller than JSON); app-level **zstd only for frames > 1 KB** (small-frame compression costs more than it saves).
- HTTP/3 (QUIC) on media presigned paths — biggest single win on lossy mobile networks (head-of-line blocking).
- Receipt coalescing (≤ 1/250 ms/conv, cumulative watermarks); typing throttle 1/3 s.
- Heartbeats 30 s with jitter (thundering-herd avoidance).

## 2. Database

- UUIDv7 (index locality); batched inbox inserts (500/statement); prepared statements everywhere (PgBouncer transaction mode compatible); covering `inbox_replay` index — no heap fetches on the resume path.
- PgBouncer transaction pooling day one; per-deployable pool budgets (bulkhead).
- Read replicas for lag-tolerant reads only (bundles, profiles, group meta).
- Partition drops instead of DELETE storms (TTL data).
- `EXPLAIN (ANALYZE, BUFFERS)` gate: any query touching `message_inbox` needs a plan review before merge (checklist in PR template).

## 3. Caching decision table

| Data | Cache? | Where | Invalidation |
|---|---|---|---|
| Group membership | ✅ | Valkey versioned set | version bump on events |
| Conversation seq | ✅ hint | Valkey write-through | PG is truth on miss |
| Prekey bundles | ❌ | — | one-time keys must not double-serve |
| Profiles | ✅ | 60 s TTL | acceptable staleness |
| Feature flags | ✅ | 30 s TTL | — |
| Message content | ❌ never | ciphertext, delivery-once semantics | — |

## 4. Media

- Client-side: images → WebP/AVIF q80; video ≤ 720p H.264/AAC; voice → Opus; blurhash placeholders (instant perceived load).
- Presigned direct-to-MinIO (services never proxy bytes); parallel multipart; ranged GETs.
- AV1 only where hardware encode exists (battery cost otherwise).

## 5. Calls

Opus DTX (silence costs ~0) + in-band FEC; simulcast (subscriber-matched layers); active-speaker-only video subscriptions in big grids; TURN/TCP-443 as last resort only (latency cost).

## 6. Clients

Chat-list virtualization; lazy media hydration; optimistic UI from outbox; delta sync by cursor (never full refetch); FTS5 incremental indexing; cold-open budget 1.5 s enforced by startup trace in CI device farm.

## 7. Go runtime

pprof continuous profiling in prod (pyroscope-class); GOGC tuned per deployable (gateway: higher heap target, fewer GC cycles at steady conns); `sync.Pool` for frame buffers; no reflection in hot paths (codegen only); race detector in CI always.

## 8. Bottleneck register (from HLD §20 — each has an owner SLI)

| # | Bottleneck | Symptom SLI | Mitigation |
|---|---|---|---|
| 1 | 1,024-member fan-out write amplification | inbox write latency | async fan-out, batching, per-sender throttle, partitioning |
| 2 | Reconnect storms | WS connect success rate | jittered backoff, resume-skip-auth, edge limits, small pods |
| 3 | Presence O(contacts) | Valkey ops/s | subscription model (30 on-screen chats only) |
| 4 | OTP spend abuse | OTP anomaly alert | §threat-model T9 controls + breaker |
| 5 | Single LiveKit node per room | room join failures | 32-cap; PTT 1-publisher cheap; V3 cascading |
| 6 | MinIO egress viral media | NIC saturation | direct presigned, per-object caps, CDN at 100k tier |
| 7 | NATS consumer backlog | delivery lag SLI | durable consumers + inbox truth (replay heals) |
| 8 | PG connection exhaustion | pool wait time | PgBouncer + budgets |
| 9 | Push provider outage | push handoff p95 | breaker + queue + inbox regardless |
| 10 | Hot conversation seq contention | per-conv accept latency | monitor; dedicated lane if ever real |
