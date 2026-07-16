# Design Patterns & Error Handling

| Doc | Server-side patterns, error taxonomy, resilience rules |
|---|---|
| Status | v1.0 |
| Scope | Go backend; client patterns in [11-clients/](../11-clients/mobile-app-architecture.md) |

## 1. Patterns we use (and why each earns its place)

| Pattern | Where | Why |
|---|---|---|
| **Modular monolith with hexagonal contexts** | core-api | Each context: `domain` (pure logic) / `ports` (interfaces) / `adapters` (PG, Valkey, NATS). Extraction-ready seams |
| **Outbox → dedupe (effectively-once)** | client outbox + server Valkey dedupe | At-least-once everywhere, UUIDv7 idempotency — NFR-15 |
| **Transactional accept, async fan-out** | chat | ACK latency decoupled from recipient count |
| **Cumulative watermarks** | receipts, sync cursors | Coalescing + crash-safe progress |
| **Fenced tokens** | PTT floor, (future) any leader-ish role | Correctness across partitions |
| **Circuit breaker** | notification-svc → FCM/APNs; SMS provider | Provider outage must not cascade; SMS breaker is also a fraud-spend cap |
| **Bulkhead** | per-deployable PG pools (PgBouncer budgets), bounded worker pools | One noisy path can't starve others |
| **Strategy behind interface** | push providers (FCM/APNs/ntfy), SMS/email OTP, object storage | Offline profile = driver swap (HLD §17.5) |
| **State machines, explicit** | call ring states, PTT floor, upload sessions, device lifecycle | Enumerated transitions, tested exhaustively |
| **Feature flags** | all deployables (Unleash-compatible) | Deploy ≠ release; canary by cohort |

**Banned:** distributed transactions/2PC (redesign the boundary instead), shared libraries containing business logic across contexts, ORMs generating runtime SQL (we use sqlc-style compile-time queries), reflection-heavy DI frameworks.

## 2. Error taxonomy (wire contract)

Every REST error and WS `error` frame carries `{code, message, retryable, retry_after?}`:

| Class | Examples | Client behavior |
|---|---|---|
| `AUTH_*` | `AUTH_TOKEN_EXPIRED`, `AUTH_DEVICE_REVOKED`, `AUTH_PIN_REQUIRED` | Refresh flow / re-register / prompt |
| `VALIDATION_*` | `VALIDATION_MESSAGE_TOO_LARGE`, `VALIDATION_EDIT_WINDOW_CLOSED` | Don't retry; surface to user |
| `RATE_*` | `RATE_LIMITED` (+`retry_after`) | Backoff exactly as told |
| `STATE_*` | `GROUP_POSTING_RESTRICTED`, `ACCOUNT_SUSPENDED`, `CALL_ALREADY_ENDED` | Refresh local state |
| `TRANSIENT_*` | `TRANSIENT_UNAVAILABLE`, `TRANSIENT_TIMEOUT` | Retry with jittered backoff |
| `INTERNAL` | anything unexpected | Retry once, then surface; always alertable server-side |

Rules: **retryable is explicit** (clients never guess from HTTP status alone); codes are stable API surface (append-only enum in proto); messages are for developers, never shown raw to users; no stack traces or internal identifiers cross the edge.

## 3. Retry & timeout policy (uniform)

| Path | Timeout | Retry |
|---|---|---|
| Client REST | 10 s | 3× exponential + full jitter (1s/2s/4s), idempotent ops only |
| Client WS send | outbox-owned | infinite with capped backoff (30 s max), survives restarts |
| gateway → core-api gRPC | 2 s | 1 retry, then fail the frame with `TRANSIENT_*` |
| core-api → PG | 5 s statement timeout | no auto-retry inside a tx; tx retried whole on serialization failure (max 2) |
| core-api → Valkey | 200 ms | 1 retry; on dedupe-check failure **fail closed** (reject send, client retries) — duplicates are worse than latency |
| NATS consumers | ack-wait 30 s | redelivery ≤ 5 → DLQ subject + alert |
| notification-svc → providers | 5 s | breaker: 5 failures/30 s → open 60 s; queue absorbs |

**Idempotency invariant:** any handler reachable via retry path must be idempotent. This is checked in review via the handler registry (each handler declares `idempotent: true|false`; non-idempotent handlers may not appear on retryable routes — CI-enforced).

## 4. Failure-mode playbook (design-level)

| Failure | Behavior by design |
|---|---|
| ws-gateway pod dies | Clients reconnect (jittered backoff) to any pod; resume replays from inbox; routes self-expire (90 s TTL) |
| core-api pod dies mid-accept | No ACK sent → client outbox retries → dedupe absorbs any partial write (insert is idempotent on UUID) |
| Valkey down | Sends fail closed (`TRANSIENT_UNAVAILABLE`); presence/typing degrade silently; **no data loss** — nothing durable lives there |
| NATS partition | Inbox is truth: deliveries stall, backlog SLI fires, replay catches up on heal |
| PG primary failover | CloudNativePG promotes replica (~30 s); writes error `TRANSIENT_*` meanwhile; clients retry from outbox |
| Push provider outage | Breaker opens; dispatch queue absorbs; messages still delivered on next app open (inbox) |
| MinIO node loss | EC 2+2 tolerates 2 disks/1 node; uploads degrade to slower writes |

## 5. Logging errors (contract with observability)

Every error crossing a context boundary is wrapped once with context (`fmt.Errorf("%w")` chain), logged **once** at the outermost handler (never log-and-rethrow), tagged with `trace_id`, `code`, `context`, `conversation_id?` — **never** message content, phone numbers, or key material (enforced by the logging schema in [09-observability/monitoring-logging-tracing.md](../09-observability/monitoring-logging-tracing.md)).
