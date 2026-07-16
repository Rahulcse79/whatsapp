# Capacity Planning

| Doc | Load model, back-of-envelope math, hardware estimates |
|---|---|
| Status | v1.0 · Upstream: [/HLD.md](../../HLD.md) §3, §22. Assumptions are inputs — re-run the math when they change. |

## 1. Load model (assumptions)

| Input | Value |
|---|---|
| Peak concurrent | 20,000 (burst design 3× = 60,000) |
| Registered / DAU | 200,000 / 60,000 (30%) |
| Messages sent per DAU/day | 40 |
| Peak factor | ×8 over daily average |
| Avg recipients per send (incl. groups) | 4 |
| Media share / avg size (post-compression) | 12% / 350 KB |
| Concurrent users in calls at peak | 6% (960 audio, 240 video) |

## 2. Derived load (the math, kept visible)

| Dimension | Calculation | Result | Headroom verdict |
|---|---|---|---|
| Messages/day | 60k × 40 | 2.4 M (~28/s avg) | trivial |
| Peak ingress | 28/s × 8 | ~250–300 msg/s | one core-api pod |
| Peak fan-out | 300 × 4 | ~1,200 deliveries/s | NATS ~10⁶/s/node |
| Inbox writes (60% offline) | 1,200 × 0.6 | ~700 rows/s batched | <10% NVMe PG box |
| Inbox storage steady-state | undelivered buffer + meta | ≤ ~50 GB | relay model (ADR-001) |
| Media ingest | 2.4M × 12% × 350 KB | ~100 GB/day | MinIO easy |
| Media store (30-d TTL) | 100 GB × 30 | ~3 TB rolling | 4-node EC pool (8 TB usable) |
| Media egress | ×2.5 downloads | ~250 GB/day ≈ 200 Mbps peak | 1–10 GbE fine |
| WS memory | 20k × ~64 KB | ~1.3 GB | one pod could hold all; 3 for HA |
| Presence ops | 20k / 30 s heartbeats | ~700 ops/s | Valkey ~150k ops/s |
| SFU egress | 960×40kbps + 240×1.2Mbps×2.5subs | < 1 Gbps | 2 LiveKit nodes |
| Push volume | ~2 M/day, peak ~100/s | negligible | — |

**Every tier ≥ 10× headroom** — this is what justifies 5 deployables on ~14 machines.

## 3. Hardware — standard profile (HLD §22)

| Role | Spec | Count |
|---|---|---|
| K8s general workers | 8 vCPU / 16 GB | 3 |
| RTC nodes | 16 vCPU / 32 GB, high egress | 2 |
| PostgreSQL | 8 vCPU / 32 GB / 1 TB NVMe | 2 (primary+replica) |
| MinIO | 4 vCPU / 8 GB / 4 TB | 4 (EC 2+2) |
| Observability | 8 vCPU / 32 GB | 1 |
| LB | managed/HAProxy | 2 |
| **Total** | | **~14 machines** · ~€700–1,000/mo commodity EU cloud; ~$3.5–5k/mo AWS-class |

## 4. Hardware — offline/single-box profile (HLD §17.5)

| Tier | Box | Capacity |
|---|---|---|
| Pilot (Compose) | 8 vCPU / 32 GB / 1 TB NVMe | ~500–2,000 concurrent |
| Production single-box (K3s) | 16–32 vCPU / 64–128 GB / 2×NVMe / 10 GbE | ~5–20k concurrent chat; calls NIC-bound (~500–1,000 legs) |

## 5. Growth triggers (watch these SLIs; act per scalability-strategy.md)

| Signal | Threshold | Action |
|---|---|---|
| ws-gateway conns/pod | > 15k sustained | add pod (HPA) |
| PG write IOPS | > 40% NVMe budget | check batch sizes → bigger box |
| Inbox oldest-undelivered-to-online | > 60 s | fan-out worker pool++ / investigate |
| Valkey ops | > 60k/s | plan Cluster migration |
| SFU node egress | > 60% NIC | add LiveKit node |
| MinIO usage | > 70% | add erasure set |
