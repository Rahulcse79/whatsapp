# Load, Chaos & Security Testing

| Doc | Load profiles, chaos scenarios, security testing |
|---|---|
| Status | v1.0 · Gate: P4 exit = SLOs green 2 weeks under the sustained profile with chaos enabled |

## 1. Load profiles (k6 + custom Go WS clients; ops/loadtest/)

| Profile | Shape | Pass criteria |
|---|---|---|
| **Sustained** | 20k concurrent WS; 300 msg/s ingress; 12% media; 6% in calls; realistic group mix | all SLOs green 24 h |
| **Burst** | ramp to 60k conns in 10 min (3× headroom, NFR-01) | connect success ≥ 99.5% during ramp; recovery ≤ 5 min |
| **Reconnect storm** | kill 1/3 gateways at 20k conns | zero loss (sequence audit), full re-connect ≤ 3 min |
| **Fan-out stress** | 50 senders → 1,024-member groups simultaneously | ACK p95 ≤ 500 ms; backlog drains ≤ 60 s; no receipt storm collapse |
| **Inbox soak** | 60% recipients offline for 24 h, then mass reconnect | replay correctness at scale; PG partition behavior |
| **Media flood** | 500 parallel 25 MB uploads + downloads | presign latency stable; API pods unaffected (isolation check) |
| **Call surge** | 300 simultaneous call setups; 500-listener PTT room | setup p95 ≤ 3 s; floor grant p95 ≤ 200 ms under churn |

Load numbers are asserted against the capacity-planning.md model — divergence > 2× in either direction is a finding (model or system is wrong; find out which).

## 2. Chaos scenarios (staging always-on lite; game days full)

| Chaos | Expectation |
|---|---|
| SIGKILL random gateway / core-api pod every 10 min under load | invisible: resume + outbox mask it |
| PG failover (kill primary) | ≤ 1 min write errors, zero loss, auto-promote |
| Valkey failover + full flush | sends fail-closed then recover; **no durable loss** (keyspace doc invariant) |
| NATS node loss / full stream wipe | delivery lag only; inbox replay heals |
| Network partition gateway↔core-api | frames error `TRANSIENT_*`, client retry, no dupes |
| Clock skew +5 min on one pod | JWT/OTP validation behavior verified; alert fires |
| Disk-full on PG node | graceful degradation, page fires, runbook works |
| Certificate expiry (staging fast-clock) | alerts at thresholds; rotation runbook |

## 3. Security testing

| Kind | Cadence | Scope |
|---|---|---|
| SAST/deps/secrets (CI) | every PR | ci-cd.md §2 |
| DAST (ZAP-class) vs staging | weekly | REST surface, authz matrix (IDOR sweeps: every endpoint × foreign IDs) |
| Fuzzing | continuous corpus in CI | protobuf frame decoder, envelope parser, multipart handling (go-fuzz) |
| Crypto integration review | pre-launch | libsignal usage, key storage, device-list signing — external reviewer |
| External pentest | P4 + annual | full scope incl. admin plane + abuse controls |
| Abuse red-team | P4 | OTP pumping economics, enumeration, spam heuristics bypass, invite scraping |
| Load-time authz | in load suite | rate limits actually bind under load (limits that only work at low QPS are decoration) |

## 4. Durability audit (the zero-loss proof, NFR-12)

Every load client numbers its messages per conversation; post-run auditor reconciles sent-ACKed vs delivered sets across all clients — any gap fails the run. This audit runs in **every** load/chaos profile above; it is the single most important assertion in the whole test estate.
