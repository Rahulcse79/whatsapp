# Runbook — Synthetic probe failing

**Alert:** `SyntheticProbeFailing` · **Severity:** page · **Fires:** `max_over_time(synthetic_probe_success[3m]) < 1` (3 consecutive failures).

## What it means
The external-vantage synthetic probe (`ops/synthetic/`) could not reach
`/healthz` (gateway) and/or `/readyz` (core-api), or the full two-client round
trip failed. This is a black-box "is the product up from outside" signal — treat
it as an outage until proven otherwise.

## Impact
Potentially total: if the probe can't connect, real users likely can't either.

## Diagnose
- Confirm blast radius: is it the probe's vantage only, or real traffic too?
  Cross-check `http_requests_total` / SLO burn and [api-availability](api-availability.md).
- Reachability from outside → inside: ingress/LB up? DNS resolving? TLS valid
  (see [cert-expiry](cert-expiry.md))?
- Pod health: `kubectl get pods -n wa-prod -l 'app.kubernetes.io/name in (ws-gateway,core-api)'`
  — CrashLoopBackOff? readiness failing?
- Which leg failed? The probe pushes `synthetic_probe_success`; check its logs
  for the gateway vs core-api curl.

## Mitigate
1. If it's an LB/ingress/DNS/cert issue: fix that layer (often the fastest real
   fix) — see [cert-expiry](cert-expiry.md) for TLS.
2. If pods are unhealthy: roll the deployment; ArgoCD rollback to the last-green
   image if a bad deploy is implicated (`argocd app rollback wa-platform-prod`).
3. If the probe itself is broken (real traffic is fine): silence this alert,
   fix the probe, do NOT stand down the incident until confirmed.

## Verify recovery
`synthetic_probe_success == 1` for 3+ runs; SLO burn subsides.

## Escalate
Immediately for a genuine outage — this is the top-line "are we down" page.

## Related
Alert `ops/alerts/platform.yaml` · probe `ops/synthetic/` · monitoring-logging-
tracing.md §5 · [api-availability](api-availability.md).
