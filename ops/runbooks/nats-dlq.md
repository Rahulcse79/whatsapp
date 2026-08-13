# Runbook — NATS DLQ growth

**Alert:** `NATSDLQGrowth` · **Severity:** page · **Fires:** any increase in `nats_dlq_total` over 10 m.

## What it means
A message was redelivered `MaxDeliver` times on a durable stream and dead-lettered
(`push.dlq` for notifications, or a delivery stream). Something downstream keeps
rejecting a message — a poison frame, a persistently-down dependency, or a bug.

## Impact
Depends on the subject: `push.dlq` → some notifications never dispatched (the
message still waits in the PG inbox, so no data loss — the user just doesn't get
woken). A delivery DLQ → live delivery gaps healed by resume replay.

## Diagnose
- Which DLQ subject? `sum by (subject) (increase(nats_dlq_total[10m]))`.
- Inspect the dead letters: `nats stream view PUSH_DLQ` (or the relevant stream)
  — is it one repeated payload (poison) or many distinct (dependency down)?
- Dependency health: for `push.dlq`, check the provider circuit
  [breaker](push-breaker.md); for delivery, check gateway/route health.

## Mitigate
1. **Dependency down** (many distinct dead letters): fix/​wait for the dependency;
   the breaker + retry will drain naturally. Optionally replay the DLQ once
   healthy: `nats stream replay ...`.
2. **Poison message** (one payload looping): remove it from the DLQ after
   capturing it for the bug ticket — never let one frame wedge the stream.
3. Confirm the consumer's `MaxDeliver`/ack-wait aren't mis-tuned causing false
   dead-lettering.

## Verify recovery
`increase(nats_dlq_total[10m]) == 0`; the source consumer's ack rate recovers.

## Escalate
Sustained growth or a delivery-path DLQ → incident + on-call lead. Any DLQ event
warrants a bug ticket even after mitigation.

## Related
Alert `ops/alerts/platform.yaml` · internal-events-nats.md · notify DLQ (T0.16) ·
[push-breaker](push-breaker.md).
