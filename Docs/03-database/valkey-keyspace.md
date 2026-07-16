# Valkey Keyspace Design

| Doc | Every key family: shape, TTL, atomicity contract |
|---|---|
| Status | v1.0 |
| Upstream | [/HLD.md](../../HLD.md) §7.4 |
| Invariant | **Nothing durable lives in Valkey.** Every key is rebuildable or expendable; Valkey loss = degraded minutes, never data loss. Deployment: Sentinel HA (3 nodes) → Cluster at 100k tier. |

## 1. Key families

| Key | Type | Value | TTL | Written by | Read by |
|---|---|---|---|---|---|
| `route:{device_id}` | string | ws-gateway pod id | 90 s, heartbeat-refreshed (30 s) | ws-gateway | core-api (delivery), gateways |
| `presence:{user_id}` | hash | `{online, last_seen, devices}` | 60 s sliding | ws-gateway | presence module |
| `presence_subs:{user_id}` | set | subscriber device ids | connection-scoped, cleaned on disconnect | core-api | presence fan-out |
| `typing:{conv_id}:{user_id}` | string | 1 | 5 s | core-api | subscribed members relay |
| `dedupe:{msg_uuid}` | string | 1 (`SET NX EX`) | 24 h | chat accept | chat accept |
| `seq_cache:{conv_id}` | string | last seq (write-through hint) | 1 h | chat | chat (fallback to PG on miss) |
| `group_members:{group_id}` | set | member user ids | none (version-invalidated) | groups module | fan-out worker |
| `group_ver:{group_id}` | string | version int | none | groups module | fan-out worker |
| `ptt:{room}:floor` | string | `{device_id, fence, granted_at}` | 2 s, heartbeat 500 ms | ptt module (Lua only) | ptt module |
| `ptt:{room}:queue` | list | waiting device ids (FIFO) | room lifetime | ptt (Lua) | ptt |
| `ptt:{room}:fence` | string | monotonic counter | room lifetime | ptt (Lua `INCR`) | ptt |
| `rl:{scope}:{key}` | string | GCRA theoretical-arrival-time | window-sized | Lua rate limiter | all edges |
| `resume:{device_id}` | string | resume-token hash | 24 h | ws-gateway | ws-gateway |
| `flag_cache:{flag}` | string | compiled rules | 30 s | flag SDK | all deployables |

## 2. Atomicity contracts (Lua-scripted, single-key-slot)

1. **Dedupe-accept:** `SET dedupe:{uuid} NX EX 86400` — one round trip decides accept/duplicate. On Valkey unavailability, **fail closed** (reject send; client outbox retries) — a duplicate visible to users is worse than a retry.
2. **PTT acquire/release/heartbeat:** one script owns all three keys (same hash tag `{room}`): check floor → grant with `INCR fence` → or enqueue with position return. No multi-step race windows exist by construction.
3. **GCRA rate check:** single script computes and stores next theoretical arrival time; returns allow/deny + retry-after.
4. **Route claim on connect:** `SET route:{device} pod EX 90` unconditionally (last-writer-wins is correct: newest connection owns the device; the old gateway's frames fail and it drops state).

## 3. Memory budget (20k concurrent)

| Family | Estimate |
|---|---|
| routes + presence + subs | 20k × ~300 B ≈ 6 MB |
| dedupe (24 h worst case) | 26 M × ~80 B ≈ 2 GB ← dominates; acceptable (correctness > memory, DS&A doc §6) |
| group membership cache | ~50k groups × avg 20 ≈ 100 MB |
| everything else | < 50 MB |

Provision 8 GB; alert at 70%; eviction policy `noeviction` (we would rather fail closed than silently lose dedupe/floor keys) — with `dedupe:*` the only family that could grow unboundedly, and it's TTL-bounded.

## 4. Failure behavior

| Scenario | Effect | Recovery |
|---|---|---|
| Valkey total loss | Sends fail-closed seconds→minutes; presence/typing blank; PTT floors drop (rooms re-acquire); routes rebuild via heartbeats | Sentinel promotes replica ≤ 10 s; no durable loss by invariant |
| Split brain | Old primary's writes discarded on rejoin | Sentinel quorum config; scripts single-key so no cross-key inconsistency |
| Hot key (`group_members` of 1,024-group during storm) | Single-shard CPU | Version-cache means reads are bursty-but-brief; monitored via keyspace profiler |
