# Kubernetes Deployment Architecture

| Doc | Cluster topology, environments, zero-downtime mechanics |
|---|---|
| Status | v1.0 · Upstream: [/HLD.md](../../HLD.md) §17 |

## 1. Cluster topology (standard profile)

```
K8s 1.33 (single region, 3 AZs where available) — K3s acceptable at small scale
├─ node pool: general ×3 (8vCPU/16GB)   envoy-gw · core-api ×2-3 · ws-gateway ×3
│                                        media-svc ×2 · notification-svc · NATS ×3
├─ node pool: rtc ×2 (16vCPU, host-network, high egress, taint rtc=true)
│                                        livekit ×2 · coturn
├─ node pool: data (dedicated, taint data=true)
│                                        PG primary+replica (CloudNativePG, anti-affinity)
│                                        Valkey ×3 (Sentinel) · MinIO ×4 (EC 2+2)
└─ observability ×1 (8vCPU/32GB)         prometheus · grafana · loki · tempo
GitOps: ArgoCD app-of-apps · Argo Rollouts · cert-manager · external-dns (CoreDNS in offline profile)
```

## 2. Workload specs (binding defaults)

| Deployable | Replicas | Requests/limits | Probes | HPA |
|---|---|---|---|---|
| core-api | 2–3 | 1 vCPU/2 GB → 4/8 | /healthz, /readyz (PG+Valkey+NATS ping) | CPU 70% + NATS queue depth |
| ws-gateway | 3 | 1/2 → 2/4 | readiness gates drain (LLD §5) | conns/pod > 15k |
| media-svc | 2 | 0.5/1 → 2/4 | + MinIO ping | CPU |
| notification-svc | 2 | 0.5/0.5 → 1/2 | + provider breaker state exposed | queue depth |
| NATS | 3 | 1/2 | built-in | never auto |
| PG | 1+1 | dedicated nodes | CloudNativePG operator | vertical only |

PodDisruptionBudgets: minAvailable 2 (gateways), 1 (core-api, NATS quorum-aware). Anti-affinity: spread across nodes/AZs for every stateful + gateway pod.

## 3. Environments (HLD §17.3)

| Env | Shape | Push/SMS |
|---|---|---|
| dev | Docker Compose or kind, full stack on laptop | mocks |
| staging | scaled-down K8s, same charts | FCM/APNs sandbox |
| prod | topology above | real |
| offline | same charts, values overlay (§17.5 doc) | ntfy / email-OTP |

Same Helm charts everywhere; only values differ; drift impossible by construction (GitOps).

## 4. Zero-downtime mechanics (NFR-14)

1. **Rollouts:** canary via Argo Rollouts (ci-cd.md §4); ws-gateway uses maxSurge=1/maxUnavailable=0 + drain (ServerHint, 60–120 s jittered reconnect).
2. **Migrations:** expand → deploy code reading both → backfill (batched, rate-limited job) → contract next release.
3. **Config changes:** flags where possible (no rollout); else rolling restart with the same drain path.
4. **Node maintenance:** cordon+drain respects PDBs; RTC nodes drained only between low-call windows (rooms don't migrate — clients rejoin).

## 5. Ingress & network

Envoy Gateway: TLS 1.3 termination, HTTP/3 for media paths, WAF rules, global GCRA, WS-aware (long-lived conn timeouts off). NetworkPolicies default-deny with the explicit matrix from [microservices.md](../02-architecture/microservices.md) §4/§6. LiveKit/coturn on host network (UDP port ranges documented in chart values).

## 6. Terraform & bootstrap

`deploy/terraform/`: cluster, node pools, DNS zone, buckets (backup target), registry, secrets bootstrap (age key distribution). Full rebuild from repo = DR RTO path ([disaster-recovery.md](disaster-recovery.md)).
