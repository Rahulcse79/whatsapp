# Runbooks & on-call

Operational runbooks, one per alert in [slos-alerts.md](../../Docs/09-observability/slos-alerts.md)
§2 (paging) / §3 (ticket). Every alert in `ops/alerts/` links to its runbook via
the `runbook:` annotation; the pager surfaces that link.

## Alert → runbook

| Alert | Severity | Runbook |
|---|---|---|
| SLO fast/slow burn (API availability) | page | [api-availability.md](api-availability.md) |
| Message E2E latency burn | page | [message-latency.md](message-latency.md) |
| Inbox delivery stuck | page | [inbox-stuck.md](inbox-stuck.md) |
| NATS DLQ growth | page | [nats-dlq.md](nats-dlq.md) |
| PG replication lag | page | [pg-replication-lag.md](pg-replication-lag.md) |
| Synthetic probe failing | page | [synthetic-probe.md](synthetic-probe.md) |
| Certificate expiry (< 3 d) | page | [cert-expiry.md](cert-expiry.md) |
| Valkey memory high | ticket | [valkey-memory.md](valkey-memory.md) |
| Push provider breaker open | ticket / page (both) | [push-breaker.md](push-breaker.md) |
| OTP request anomaly | ticket | [otp-anomaly.md](otp-anomaly.md) |

DR-drill results are logged under `ops/runbooks/drills/` (disaster-recovery.md §4).

## On-call

- **Severity:** `page` = user-impacting, wake someone now; `ticket` = fix in
  business hours. Definitions in slos-alerts.md §2/§3.
- **Escalation:** primary on-call ack ≤ 5 min → if no ack, secondary → on-call
  lead. Both-provider push outage and any synthetic-probe/availability page are
  lead-notify from the start.
- **The one invariant to protect:** zero message loss (NFR-12). If any mitigation
  risks a server-ACKed message, stop and escalate — latency is recoverable, loss
  is not.
- **Kill-switches** (flag console, core-api-lld §5) pause a subsystem without a
  deploy — the fastest mitigation for load/abuse pages.
- **Rollback:** `argocd app rollback wa-platform-<env>` is instant (image
  rollback; DB contract phases lag ≥ 1 release so it never fights migrations).

## Postmortems

Blameless [postmortem](postmortem-template.md) required within 48 h of any page
> 15 min or any durability event. The durability-audit section is mandatory.
