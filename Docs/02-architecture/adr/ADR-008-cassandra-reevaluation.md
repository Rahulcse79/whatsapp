# ADR-008: Cassandra re-evaluation — the partitioned Postgres inbox stays

Status: **Accepted** · Task: T15.03 · Related:
[ADR-001](ADR-001-relay-model.md) (relay model),
[ADR-007](ADR-007-kafka-reevaluation.md) (the NATS counterpart) ·
Upstream: HLD §16.3

## Context

T15.03 is conditional: *"IF the partitioned Postgres inbox is the bottleneck,
prototype a Cassandra inbox behind the InboxWriter port; otherwise document why
Postgres stays."* This ADR takes the **otherwise** branch.

As with [ADR-007](ADR-007-kafka-reevaluation.md), there are two independent
reasons and the second does not depend on any measurement.

## Reason 1 — the condition was never met

[T15.01](../../07-scalability/capacity-baseline-T15.01.md) produced no evidence
that the inbox is a bottleneck, and could not: the target-scale run is blocked,
and the server exposes no `pgxpool` statistics or inbox-write instrumentation.
Swapping the hottest table in the system onto a new datastore on an unmeasured
suspicion would be the worst possible outcome of this phase.

## Reason 2 — the inbox is a *drain-to-empty queue*, which is Cassandra's
weakest workload

### What the inbox actually is

`message_inbox` (migration `000005_messaging.up.sql`) is HASH-partitioned on
`recipient_device_id` into 16 partitions, with

```sql
PRIMARY KEY (recipient_device_id, conversation_id, seq, msg_uuid)
```

and no foreign keys, deliberately, for partition locality and write volume. The
three access paths (`internal/chat/adapters/pg.go`) are:

| Path | Query shape |
|---|---|
| Fan-out write | batch `INSERT`, idempotent on the PK |
| Resume/replay | `WHERE recipient_device_id = $1 AND (conversation_id, seq) > (cursor) ORDER BY conversation_id, seq LIMIT n` |
| Ack | `DELETE … WHERE recipient_device_id = $1 AND conversation_id = $2 AND seq <= $3` |

**Rows are deleted the moment a device acknowledges them.** The 30-day
`expires_at` is only a backstop for devices that never come back. This follows
directly from ADR-001: the server keeps no message archive, so the inbox is a
buffer sized by *undelivered* messages — capacity planning puts its steady state
at **≤ ~50 GB**, and that figure does not grow with cumulative usage.

### Why that rejects Cassandra

**a) Delete-on-ack means tombstones, and this is a range-delete queue.** Every
acknowledgement issues a range delete over a clustering range inside exactly the
partition the device reads from most. In Cassandra those become range
tombstones that live for `gc_grace_seconds` (10 days by default), inflate read
latency on that partition until compaction reclaims them, and can trip
tombstone thresholds outright. A queue that empties itself is the textbook
Cassandra anti-pattern; it is the one workload where Postgres's in-place
`DELETE` + autovacuum is straightforwardly better.

**b) Postgres is already partitioned the way Cassandra would partition it.**
Partition key `recipient_device_id`, clustering `(conversation_id, seq)` — the
replay query is a single-partition, clustering-ordered range scan, i.e. exactly
the shape you would design for Cassandra. The horizontal-distribution lever
Cassandra sells is therefore *already available here*: raise the hash modulus,
or move partitions to their own tablespaces/instances, with no new datastore and
no rewrite of the access layer.

**c) The read path joins.** Replay `LEFT JOIN`s `devices` so a message from a
since-deleted sender device still replays (with an empty sender id) instead of
black-holing. Cassandra has no joins: this would have to be denormalised into
the inbox row at write time, which adds a write-path field and re-opens the
exact deleted-sender edge case the join was written to handle.

**d) The dataset is bounded and single-region.** Cassandra earns its operational
cost on unbounded, append-mostly, multi-DC datasets. This one is bounded (~50 GB
steady state), deleted eagerly, and single-region. None of the three levers
apply.

### What Cassandra would cost

A new stateful tier with its own repair, compaction and topology operations,
plus the loss of transactional writes across `message_inbox` and the tables the
accept path touches in the same transaction. Against a table that today fits on
the 8 vCPU / 32 GB / 1 TB NVMe primary already in the capacity plan.

## Decision

**The partitioned Postgres inbox stays.** No Cassandra prototype is built.
T15.03 closes as evaluated-and-rejected, not skipped.

## Revisit triggers

Observable conditions that would genuinely reopen this — none measurable until
the T15.01 instrumentation gap is closed:

1. **Write saturation** — inbox `INSERT` batch p99 degrading while the PG box is
   not CPU- or IO-bound at the device layer, i.e. contention rather than
   hardware.
2. **Vacuum can't keep up** — dead-tuple ratio on `message_inbox` partitions
   climbing monotonically under steady load, with autovacuum already tuned.
   *(This is the honest failure mode for the current design, and the one to
   watch first — note it argues for tuning or more partitions before it argues
   for Cassandra.)*
3. **Storage outgrowing the plan** — steady-state undelivered buffer exceeding
   the ~50 GB estimate by an order of magnitude, which would mean the relay
   assumption in ADR-001 no longer holds.
4. **A real multi-region write requirement** — the one lever that Postgres
   partitioning cannot answer and Cassandra genuinely can.

Trigger 4 is the only one that leads to Cassandra rather than to tuning.

## Consequences

- ✅ No new stateful tier; the existing primary + replica stands.
- ✅ `fanout.InboxWriter` and `chat.Inbox` remain the swap seams, so a future
  alternative store stays a contained change behind two interfaces.
- ⚠️ Triggers 1–2 are unmeasurable until `pgxpool.Stat()` gauges and an
  inbox-write histogram exist — the same prerequisite ADR-007 identifies for
  NATS. **That instrumentation task is worth more than either evaluation.**
