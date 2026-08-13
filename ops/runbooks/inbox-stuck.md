# Runbook — Inbox delivery stuck

**Alert:** `InboxDeliveryStuck` · **Severity:** page · **Fires:** `inbox_oldest_undelivered_online_s > 60` for 5 m.

## What it means
A message is committed in the PG `message_inbox` for a device that is **online**
(has a live gateway route) but hasn't been delivered for over a minute. The relay
model guarantees the message is not lost — but the live delivery path
(`msg.accepted → dev.{device}.out → gateway → client`) has stalled for at least
one online device.

## Impact
Users see sent messages not arriving in real time (they still arrive on
reconnect/resume, so this is latency, not loss). Widespread → looks like an
outage.

## Diagnose
- Scope: `topk(10, inbox_oldest_undelivered_online_s)` — one device or many?
- NATS transit healthy? Check `nats_dlq_total` and consumer lag on `dev.*.out`;
  a NATS backlog (bottleneck #7) is the usual cause.
- Gateway pump: are outbound queues overflowing (slow-consumer 1013 closes
  spiking)? `rate(ws_slow_consumer_close_total[5m])`.
- Routes fresh? `route:{device_id}` in Valkey should refresh every 30 s; a
  Valkey issue orphans routes → see [valkey-memory](valkey-memory.md).

## Mitigate
1. If NATS is the cause: restart the stuck consumer / roll the affected
   `notification-svc`/gateway pod — inbox is truth, redelivery + dedupe are safe.
2. If a gateway pod is wedged: `kubectl delete pod` it; clients resume and the
   backlog replays (the relay-model invariant — this is *safe*, never lossy).
3. If Valkey routes are stale: confirm Valkey health first, then bounce gateways
   so routes rebuild from heartbeats.

## Verify recovery
`inbox_oldest_undelivered_online_s` drops back under 60 s; synthetic probe E2E
latency (`msg_e2e_latency`) returns to SLO.

## Escalate
> 15 min or growing across many devices → page the on-call lead; open an incident
and write a [postmortem](postmortem-template.md) (durability audit mandatory).

## Related
Alert `ops/alerts/platform.yaml` · design HLD §8.2, §20 bottleneck #7 ·
[nats-dlq](nats-dlq.md).
