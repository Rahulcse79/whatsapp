# Runbook — Message E2E latency SLO burn

**Alert:** `MsgLatencySLOFastBurn` · **Severity:** page · **Fires:** message end-to-end p95 > 500 ms (synthetic probe + real traffic burn).

## What it means
The send→deliver latency SLO is burning: messages are being relayed slower than
the 500 ms p95 target. Measured both by the synthetic two-client probe
(`msg_e2e_latency`) and by real-traffic delivery timing.

## Impact
Chat feels laggy — sends take noticeably long to land. Not loss (the relay model
still delivers), but a core-UX SLO breach.

## Diagnose
- Which stage? Break down: accept (chat) → NATS transit → gateway pump → client.
  - Accept slow: `chat_accept_duration` p95, per-conversation seq contention
    (bottleneck #10 hot conversation).
  - Transit slow: NATS consumer lag on `dev.*.out`; see [nats-dlq](nats-dlq.md).
  - Delivery slow: gateway outbound queue depth, slow-consumer closes.
- Correlate with load (a sustained/fan-out spike) or a dependency (PG write
  amplification #1, PgBouncer wait #8).

## Mitigate
1. **Hot conversation / seq contention**: identify the conversation; worst case
   give it a dedicated lane; confirm big-group fan-out is async off the ACK path.
2. **NATS backlog**: scale consumers / roll the wedged pod (safe — inbox is
   truth).
3. **PG saturation**: relieve the primary (see [pg-replication-lag](pg-replication-lag.md),
   bottleneck #8); confirm batched inbox writes + coalesced receipts are on.
4. **Load**: verify autoscaling engaged; shed non-critical load via a kill-switch.

## Verify recovery
`msg_e2e_latency` p95 back under 500 ms; burn-rate alert clears.

## Escalate
Not recovering in ~10 min, or paired with availability burn → incident + on-call
lead.

## Related
Alert `ops/alerts/slo-burn.yaml` · slos-alerts.md §1 · HLD §8.2, §20, §21 ·
[inbox-stuck](inbox-stuck.md).
