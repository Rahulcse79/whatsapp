# Runbook — API availability SLO burn

**Alerts:** `SLOFastBurnAPIAvailability` (14.4× budget, 1 h & 5 m) · `SLOSlowBurnAPIAvailability` (6×, 6 h & 30 m). **Severity:** page.

## What it means
The API availability SLO's error budget is burning fast. Multi-window burn-rate
(SRE-workbook): the fast alert means a sharp, ongoing failure; the slow alert
means a persistent low-grade error rate eating the budget. Both page because they
are user-impacting.

## Impact
Elevated 5xx / `TRANSIENT_*` on the REST + gRPC surface — failed sends, logins,
media presigns. The fast burn is felt as an outage; the slow burn as flakiness.

## Diagnose
- Where: `sum by (route,status) (rate(http_requests_total{status=~"5.."}[5m]))`
  → which endpoint/deployable and which status.
- Latency vs errors: is it 5xx (errors) or timeouts (saturation)? Check
  `http_request_duration` p95 and `http_requests_in_flight`.
- Common causes: bad deploy (correlate with the last Argo Rollout), a dependency
  down (PG pool exhaustion, Valkey, NATS), or load (bottleneck #8 pool
  exhaustion). Check `pgbouncer_wait` and Valkey/NATS health.

## Mitigate
1. **Bad deploy** → `argocd app rollback wa-platform-<env>` (instant image
   rollback; DB contract phases are ≥ 1 release behind so this never fights
   migrations).
2. **Dependency**: follow the specific runbook — PG pool → scale/kill blockers;
   [valkey-memory](valkey-memory.md); [nats-dlq](nats-dlq.md);
   [pg-replication-lag](pg-replication-lag.md).
3. **Load**: confirm HPA scaled; flip a kill-switch to shed a non-critical path
   (e.g., pause group creation) via the flag console — no deploy needed.

## Verify recovery
5xx rate back under SLO; burn-rate alert clears in both windows.

## Escalate
Fast burn not mitigated in ~10 min → incident + on-call lead; postmortem required
for any page > 15 min.

## Related
Alert `ops/alerts/slo-burn.yaml` · slos-alerts.md §1–§2 · kill-switches
(core-api-lld §5) · HLD §20.
