# ADR-003: NATS JetStream over Kafka

Status: **Accepted** · Upstream: HLD correction #3, §6

## Context

We need durable at-least-once transit for message delivery, push dispatch, and domain events at ~1,200 deliveries/s peak (designed ≥ 10×). Kafka was proposed twice (raw requirements; 2026-07-16 stack proposal).

## Decision

**NATS JetStream 2.11**, 3-node RAFT cluster. Per-device subjects (`dev.{id}.out`), durable consumers for dispatch pipelines. **PostgreSQL inbox remains the source of truth; NATS is transit** — this makes broker loss a latency event, not a data-loss event.

## Alternatives

- **Kafka:** built for partition-ordered, replayable, multi-consumer logs at 10⁵–10⁷ msg/s. Our workload is per-device addressed delivery at ~10³/s. Costs: broker+controller fleet, partition/rebalance management, JVM tuning — the heaviest ops item in the old stack for capability we don't use. Rejected; documented swap-in at ~1M concurrent if event archival/replay becomes a requirement (HLD §16.3).
- **RabbitMQ:** fine broker; weaker horizontal scaling story and stream semantics. Rejected.
- **Valkey streams:** would overload the cache tier's failure domain. Rejected.

## Consequences

- ✅ Three tiny pods, ms latency, subject-per-device fits routing exactly.
- ⚠️ Smaller ecosystem than Kafka; fewer connectors (we need none).
- Revisit trigger: sustained > 50k msg/s or a hard event-archival requirement.
- **Re-evaluated post-build by [ADR-007](ADR-007-kafka-reevaluation.md) (T15.02): NATS stays.** That ADR replaces the trigger above with conditions observable from instrumentation.
