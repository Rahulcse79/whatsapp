# ADR-007: Kafka re-evaluation — NATS stays

Status: **Accepted** · Task: T15.02 · Re-affirms and supersedes the revisit
clause of [ADR-003](ADR-003-nats-over-kafka.md) · Upstream: HLD §16.3

## Context

T15.02 is conditional: *"IF load shows NATS is the bottleneck for a specific
stream, introduce Kafka for that stream behind the existing port; otherwise
document why NATS stays."* This ADR takes the **otherwise** branch and records
why, with the evidence available now that the system is built (ADR-003 was
decided before any of it existed).

Two independent reasons, either of which alone is sufficient.

## Reason 1 — the condition was never met

[T15.01](../../07-scalability/capacity-baseline-T15.01.md) did not find a NATS
bottleneck. It could not: the target-scale run is blocked (five profiles,
including both gate profiles, abort on a missing codec bundle), and the server
carries **no NATS instrumentation at all** — no consumer lag, no JetStream
pending count, nothing. There is no measurement pointing at NATS, so the
condition that would open this task has not fired.

Adopting Kafka on an unmeasured suspicion is precisely the failure mode the
"evidence-gated" wording exists to prevent.

## Reason 2 — the workload is structurally the wrong shape for Kafka

This is the stronger reason, because it does not depend on a measurement.

### What actually runs over NATS today

Eleven subjects across ten bounded contexts. **Exactly one is durable.**

| Subject | Context | Transport | Purpose |
|---|---|---|---|
| `dev.{id}.out` | chat | core NATS | live delivery to a connected device |
| `dev.{id}.receipt` | chat | core NATS | receipt relay |
| `dev.{id}.call` | calls, ptt | core NATS | ring / floor signalling |
| `pres.{user}` | presence | core NATS | presence |
| `typ.{user}` | presence | core NATS | typing |
| `group.events.{id}` | groups | core NATS | membership/settings changes |
| `channel.{id}.post` | channels | core NATS | new-post nudge |
| `user.events` | devices | core NATS | device add/revoke |
| `media.lifecycle` | media | core NATS | GC lifecycle |
| `analytics.event` | analytics | core NATS + queue group | metadata rollups |
| **`push.dispatch`** + `push.dlq` | notify | **JetStream** | durable push work queue |

Verified by reading every `internal/*/adapters/nats.go`; only
`internal/notify/adapters/nats.go` touches JetStream.

### Why that shape rejects Kafka

**a) The addressing model is per-entity subjects, not partitions.** Delivery is
addressed to *one device* — `dev.{device_id}.out` — and consumed by whichever
gateway currently holds that socket. Kafka has no equivalent: you would need
either a partition per device (20k+ partitions, far past comfortable) or a
routing layer that maps devices onto partitions and re-routes on reconnect —
i.e. reimplementing NATS subjects on top of Kafka.

**b) Durability is deliberately *not* in the broker.** `internal/chat/adapters/
nats.go` states the invariant in the code: the PostgreSQL inbox plus resume
replay is the durability mechanism, and NATS carries only the live push to
already-connected devices. Kafka's central value — a retained, replayable log —
duplicates a guarantee Postgres already provides, and would make broker loss a
data-consistency question instead of the latency-only event it is today.

**c) The one durable stream wants a queue, not a log.** `push.dispatch` needs
per-message ack, redelivery with a bounded `MaxDeliver` (6), a 30 s `AckWait`,
and a dead-letter subject — all first-class in JetStream and all things Kafka
lacks natively. On Kafka this becomes offset tracking plus a retry-topic ladder
plus a DLQ topic: strictly more moving parts for strictly less capability.

**d) Nothing consumes a replayable history.** Kafka earns its operational cost
when several independent consumer groups re-read the same log at different
offsets. No context here does that. `analytics.event` is the closest, and it
uses a queue group precisely so exactly one pod handles each event.

### What Kafka would cost

A broker + controller fleet, partition and rebalance management, and JVM tuning
— against three small NATS pods today. ADR-003 already called this "the heaviest
ops item in the old stack for capability we don't use"; having built the system,
that assessment holds and is now backed by the subject inventory above.

## Decision

**NATS stays**, for every subject including `push.dispatch`. No Kafka is
introduced. T15.02 closes as evaluated-and-rejected, not as skipped.

## Revisit triggers

ADR-003's trigger ("sustained > 50k msg/s") is not directly observable today.
Replacing it with conditions that are, once the T15.01 instrumentation gap is
closed:

1. **Sustained consumer lag on `push.dispatch`** — durable `notify` consumer
   `NumPending` growing monotonically for > 15 min at steady offered load, with
   the notification-svc pods not CPU-bound (i.e. the broker, not the worker, is
   the constraint).
2. **Redelivery pressure** — `push.dlq` depth rising while individual push
   drivers report healthy, meaning `MaxDeliver` is being exhausted by broker
   behaviour rather than provider failures.
3. **A genuine replay requirement** — a second, independent consumer of the
   delivery or analytics stream that must re-read history at its own offset
   (e.g. a compliance archive or a rebuildable read model). This is the one that
   would actually change the shape of the answer.
4. **Fan-out subject-cardinality pain** — NATS server memory or match latency
   degrading as `dev.*` subject count grows toward 1M concurrent devices
   (HLD §16.3's original swap-in point).

Any single trigger reopens this ADR. Triggers 1 and 2 are unobservable until the
NATS instruments listed in the T15.01 report exist — **adding those instruments
is the real prerequisite, and is worth more than this evaluation.**

## Consequences

- ✅ No new infrastructure; the three-pod broker tier stands.
- ✅ The `notify.DLQPublisher` / consumer ports remain the swap seam, so a future
  Kafka adapter for `push.dispatch` alone stays a contained change.
- ⚠️ Triggers 1–2 stay unmeasurable until NATS instrumentation lands; until then
  this decision rests on Reason 2 (structural fit), not on telemetry.
