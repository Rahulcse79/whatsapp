# Chaos suite (`ops/chaos/`)

Chaos Mesh scenarios from load-and-chaos-testing.md §2. **Two tiers:**

- **`always-on-lite.yaml`** — Chaos Mesh `Schedule`s that run continuously in
  staging (ArgoCD-synced), so resilience is proven every day.
- **`game-day.yaml`** — destructive, stateful scenarios applied **manually**
  during a scheduled game day under full load.

The single pass criterion across every scenario is the **zero-loss durability
audit** (`ops/loadtest/auditor.js`, NFR-12): while chaos fires under the sustained
load profile, `msgs_lost==0` must hold — that is the proof the relay model masks
failure (the PG inbox is truth; gateways/NATS are transit).

## Scenario → manifest → expectation

| §2 scenario | Tier | Where | Expectation |
|---|---|---|---|
| SIGKILL random gateway every 10 min | lite | `gateway-kill` (PodChaos) | invisible — resume + outbox mask it |
| SIGKILL random core-api pod | lite | `core-api-kill` (PodChaos) | in-flight → `TRANSIENT_*`, retry, no loss |
| Network partition gateway↔core-api | lite | `gateway-core-partition` (NetworkChaos) | fail-closed `TRANSIENT_*`, dedupe absorbs retries, no dupes |
| PG failover (kill primary) | game-day | `pg-primary-failover` (PodChaos) | ≤ 1 min write errors, zero loss, auto-promote |
| Valkey failover + flush | game-day | `valkey-failover-flush` (PodChaos) | fail-closed then recover; **no durable loss** |
| NATS node loss / stream wipe | game-day | `nats-node-loss` (PodChaos) | delivery lag only; inbox replay heals |
| Clock skew +5 min on one pod | game-day | `clock-skew-core-api` (TimeChaos) | JWT/OTP skew behaviour verified; alert fires |
| Disk-full on PG node | game-day | manual (fill the PG PVC) | graceful degradation, page fires, runbook works |
| Certificate expiry | game-day | manual (staging fast-clock + short-TTL cert) | alerts at thresholds; rotation runbook |

Disk-full and certificate-expiry have no clean Chaos Mesh primitive, so they are
manual game-day procedures (the last two rows).

## Running

Always-on lite is applied by ArgoCD with the platform. Arm a game-day scenario
under load:

```bash
# 1) start the sustained load + a recipient cohort so the durability audit runs
k6 run -e WS_URL=wss://staging.wa/v1/ws -e TOKENS=... ops/loadtest/sustained.js &

# 2) fire one game-day scenario
kubectl apply -f ops/chaos/game-day.yaml   # or a single doc via `yq`/`kubectl -f -`

# 3) after the run, the auditor's msgs_lost==0 threshold is the pass/fail
```

Requires the Chaos Mesh controller (`chaos-mesh.org/v1alpha1`) installed in the
cluster; these CRs are validated for well-formed YAML in CI (their CRDs are
cluster-side, so kubeconform skips them like the other CRD-based manifests).
