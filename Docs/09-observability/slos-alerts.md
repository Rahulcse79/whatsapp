# SLOs & Alerting

| Doc | SLIs/SLOs, burn-rate alerts, paging policy |
|---|---|
| Status | v1.0 · Philosophy: page on user-impacting symptoms (SLO burn), ticket on causes. Every page maps to a runbook in ops/runbooks/. |

## 1. SLOs (binding — NFR cross-refs)

| SLI | SLO | NFR |
|---|---|---|
| API availability (5xx ratio) | 99.9% monthly | NFR-11 |
| Message E2E latency (online↔online) | p95 ≤ 500 ms | NFR-05 |
| WS connect success | ≥ 99.9% | NFR-09 |
| Message durability | zero ACKed-loss (measured by probe sequence audit) | NFR-12 |
| Call setup | p95 ≤ 3 s | NFR-06 |
| PTT floor grant | p95 ≤ 200 ms | NFR-08 |
| Push handoff | p95 ≤ 2 s | — |

Error budget policy: budget burn > 50% mid-month ⇒ feature freeze on the burning path, reliability work takes the sprint.

## 2. Paging alerts (multi-window burn rate, SRE-workbook style)

| Alert | Condition |
|---|---|
| SLO fast burn | 14.4× budget burn over 1 h AND 5 m (any SLO above) |
| SLO slow burn | 6× over 6 h AND 30 m |
| Inbox delivery stuck | `inbox_oldest_undelivered_online_s > 60` for 5 m |
| NATS DLQ growth | any `nats_dlq_total` increase |
| PG replication lag | > 10 s for 5 m |
| PG pool exhaustion | PgBouncer wait > 100 ms p95 for 5 m |
| Valkey memory | > 80% or `noeviction` errors |
| SFU room loss | p95 packet loss > 5% for 5 m |
| Push breaker open | > 5 m (both providers ⇒ page; one ⇒ ticket) |
| OTP anomaly | request rate > 5× 7-day seasonal baseline (fraud/spend signal) |
| Cert expiry | < 14 d (ticket), < 3 d (page) |
| Backup failure | WAL archive gap > 15 m (page); nightly base failure (ticket) |
| Synthetic probe failure | 3 consecutive failures any probe (page) |

## 3. Ticket-level (non-paging)

Single pod crash-looping · HPA at max · MinIO capacity > 70% · dedupe-store > 70% memory · replay depth anomaly · rate-limit hit spikes (abuse review) · GlitchTip new crash signature > 0.1% sessions.

## 4. On-call expectations

Small-team rotation (NFR-27): business-hours ticket triage daily; 24/7 paging only for §2. Every page: runbook link in the alert annotation; postmortem (blameless, 48 h) for any page > 15 min or any durability event — template in ops/runbooks/postmortem-template.md.
