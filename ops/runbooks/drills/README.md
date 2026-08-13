# DR drills — results log

This directory is where **disaster-recovery.md §4** says drill results are logged.
A backup that isn't restore-tested doesn't exist; a runbook that hasn't been
executed is fiction. Each drill run drops a dated result file here so the
recovery capability is *evidenced*, not assumed.

## Drill catalogue (cadence from disaster-recovery.md §4)

| Drill | Runbook | Cadence | Proves |
|---|---|---|---|
| Site-loss game day | [dr-game-day.md](dr-game-day.md) | Quarterly (alt) | Rebuild from repo + backups within **RTO ≤ 60 min** |
| PITR restore | [pitr-restore.md](pitr-restore.md) | Quarterly | PG point-in-time restore to scratch + validation, **RPO ≤ 5 min** |
| Secrets-compromise rotation | (dr-game-day.md §7 variant) | Semi-annual | Full SOPS re-key + CA rotation, service continuity |
| Chaos-kill gateway | CI staging (ops/chaos always-on-lite) | Every deploy | Zero-loss under a ws-gateway kill (GATE P0) |

The game-day **chaos injections** themselves live in
[`ops/chaos/game-day.yaml`](../../chaos/game-day.yaml) (pg-primary-failover,
valkey-failover-flush, nats-node-loss, clock-skew-core-api).

## Logging a run

Copy the scorecard at the bottom of the drill's runbook into a new file named:

```
ops/runbooks/drills/YYYY-MM-DD-<drill>.md
```

e.g. `2026-09-15-site-loss.md`. Fill the stopwatch table, attach the pass/fail
against RTO/RPO, list any follow-up tickets, and get the two named sign-offs
(incident lead + reviewer). A drill with an unmet target is **not** a failure to
hide — it is the finding; open a ticket and re-drill.

> These result files are the T4.07 evidence and the P4/go-live gate input. Until
> at least one green site-loss run and one green PITR run exist here, T4.07 stays
> `[ ]`.
