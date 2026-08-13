# Runbook — PostgreSQL replication lag

**Alert:** `PGReplicationLag` · **Severity:** page · **Fires:** replica lag > 10 s for 5 m.

## What it means
The read replica is falling behind the primary. Read-heavy endpoints served from
the replica (key bundles, profiles — §21) may return stale data, and a failover
right now would lose up to the lag window (RPO risk).

## Impact
Stale reads: a just-published prekey/profile update may not appear on the replica
yet. Sends are unaffected (they hit the primary). Elevated risk if the primary
then fails.

## Diagnose
- Lag source on the primary: `SELECT client_addr, state, pg_wal_lsn_diff(pg_current_wal_lsn(), replay_lsn) AS bytes FROM pg_stat_replication;`
- Replica applying WAL? Long-running query blocking replay:
  `SELECT now()-query_start, query FROM pg_stat_activity WHERE state<>'idle' ORDER BY 1 DESC LIMIT 5;` on the replica.
- Write spike on the primary (bottleneck #1: 1,024-group fan-out) or disk I/O
  saturation on the replica? Check node I/O + `pg_stat_bgwriter`.

## Mitigate
1. Kill a long replica query that's blocking WAL replay (`pg_terminate_backend`).
2. If a write storm on the primary: confirm the async fan-out worker + per-sender
   big-group rate limits are engaged; shed load if needed.
3. If the replica can't keep up (undersized/I/O-bound): temporarily route the
   read-heavy endpoints back to the primary; scale the replica's I/O.
4. Do NOT trigger a failover while lag is high unless the primary is actually
   failing — that maximizes RPO loss.

## Verify recovery
Lag < 10 s and trending to ~0; `pg_stat_replication.replay_lsn` catches up.

## Escalate
Lag growing unbounded, or a primary failure while lagging → DBA/on-call lead;
see disaster-recovery.md for the failover/RPO procedure.

## Related
Alert `ops/alerts/platform.yaml` · database-design.md · HLD §20 #1/#8 · backup-
recovery.md.
