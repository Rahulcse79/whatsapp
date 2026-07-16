# Scalability Strategy

| Doc | Vertical + horizontal plans; the 20k → 100k → 1M ladder |
|---|---|
| Status | v1.0 · Upstream: [/HLD.md](../../HLD.md) §16 |
| Invariant | **No tier requires re-architecture.** Climbing = more nodes + partitioning planned from day one + (at 1M) one datastore swap for one table. |

## 1. Per-component scaling profile

| Component | Vertical ceiling | Horizontal mechanism | Coordination cost |
|---|---|---|---|
| ws-gateway | ~250k conns/pod possible — but prefer horizontal past 50k (blast radius) | Add pods; L4 least-conn; **no stickiness** (route table + per-device subjects) | none — that's the design |
| core-api | 32 vCPU pure headroom | HPA on CPU + queue depth (stateless) | none |
| PostgreSQL | **The big vertical lever**: one primary rides to ~1M-concurrent workload (96 vCPU/768 GB + partitioning + PgBouncer) | Read replicas (profiles/keys/groups); inbox hash partitions; user_id sharding only at extreme scale | replica-lag monitoring; shard router at 1M |
| Valkey | Memory only (single-threaded ~150k ops/s core) | Sentinel → Cluster (slot-sharded); keys already hash-tagged per room/user | client cluster mode |
| NATS | ~1M msg/s/node | Add RAFT nodes → leafnodes per region | minimal |
| LiveKit | 10 GbE NIC-bound | Pool nodes (room-per-node); regional pools later | built-in registry |
| MinIO | disks/NIC | Add erasure-set pools | rebalance window |
| media/notification | trivial | HPA | none |

## 2. The ladder (what actually changes)

| Tier | 20k (now) | 100k | 1M |
|---|---|---|---|
| Gateways | 3 pods | ~10 pods, same design | regional pools across AZs |
| core-api | 2–3 pods | **extract `chat` + `presence`** (the pre-planned split, microservices.md §5) | cell-based: users sharded into self-contained cells |
| PostgreSQL | primary + replica | bigger box + 2 replicas + aggressive partitioning | shard inbox by user_id **or** swap inbox table to ScyllaDB; all else stays PG |
| Valkey | Sentinel ×3 | Cluster 3 shards | cluster per cell |
| NATS | 3 nodes | 5 nodes | per-region + leafnodes (or Kafka if archival requirement appears — ADR-003 trigger) |
| LiveKit | 2 | 5–8 + regional pool | multi-region, geo-routed |
| MinIO | 4 (EC 2+2) | 8 + CDN for media GETs | multi-site replication + CDN |
| Region model | single + DR | single primary + media CDN | active-active gateways/media; **single-home per user** for messaging state |
| Team | 2–4 eng | +SRE rotation | platform team |

## 3. Why cell-based at 1M (decided now, built later)

Cells (self-contained user shards: gateway+core+PG+Valkey per cell) bound blast radius, keep the inbox hot set per-cell small, and avoid global coordination. Cross-cell messaging = inter-cell NATS bridging on `dev.{id}.out`. The seams that make this cheap later: per-device subjects, no cross-user transactions anywhere in the schema, route table already device-scoped.

## 4. Anti-goals

No premature sharding (a 1-primary PG with partitioning beats a badly-sharded fleet); no multi-region active-active for messaging state before 1M (conflict complexity); no service extraction before a split trigger fires (microservices.md §5).
