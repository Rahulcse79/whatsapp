# ops/

No artifacts yet (blueprint phase).

| Dir | Will contain | Blueprint |
|---|---|---|
| `dashboards/` | Grafana dashboards as code (catalog of 7) | [Docs/09-observability/monitoring-logging-tracing.md](../Docs/09-observability/monitoring-logging-tracing.md) §4 |
| `alerts/` | Prometheus/Alertmanager rules (paging + ticket tiers) | [Docs/09-observability/slos-alerts.md](../Docs/09-observability/slos-alerts.md) |
| `runbooks/` | Per-alert runbooks, DR procedures, drill logs, postmortem template | [Docs/08-devops/disaster-recovery.md](../Docs/08-devops/disaster-recovery.md) |
| `loadtest/` | k6 + Go WS load clients, durability auditor | [Docs/10-testing/load-and-chaos-testing.md](../Docs/10-testing/load-and-chaos-testing.md) |

First task: T0.23 in [task-breakdown.md](../Docs/12-planning/task-breakdown.md).
