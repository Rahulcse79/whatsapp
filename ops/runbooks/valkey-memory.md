# Runbook — Valkey memory high

**Alert:** `ValkeyMemoryHigh` · **Severity:** ticket · **Fires:** used memory > 80% or `noeviction` errors.

## What it means
Valkey is nearing its memory ceiling. The eviction policy is deliberately
`noeviction` — we fail closed rather than silently drop dedupe/floor/route keys
(keyspace invariant). So at the ceiling, WRITES start erroring rather than losing
data.

## Impact
Approaching the limit → risk of write failures on the hot path: dedupe
(`dedupe:*` is the only unbounded-growth family, TTL-bounded), PTT floor keys,
routes, rate-limit state. Sends fail-closed if it hits the wall.

## Diagnose
- `redis-cli INFO memory` (used_memory, maxmemory, fragmentation).
- Biggest families: `redis-cli --bigkeys` / keyspace profiler — is `dedupe:*`
  or an HLL/`analytics:hll:*` sketch growing? A stuck TTL?
- Any `OOM`/`noeviction` errors in the app logs (fail-closed sends)?

## Mitigate
1. If a key family lost its TTL (a bug): re-apply TTLs / delete the offending
   family after confirming it's safe (dedupe keys are TTL-bounded and safe to
   trim; floor/route keys are NOT — they'd drop live state).
2. If it's organic growth: scale Valkey memory (it's provisioned at 8 GB, alert
   at 70% — headroom exists) or add a shard.
3. Do NOT switch to an eviction policy — failing closed is the design choice.

## Verify recovery
Used memory back under 70%; no `noeviction` errors.

## Escalate
If sends are failing-closed (at the wall) this becomes a **page** — treat as an
availability incident; see [api-availability](api-availability.md).

## Related
Alert `ops/alerts/platform.yaml` · valkey-keyspace.md · HLD §20.
