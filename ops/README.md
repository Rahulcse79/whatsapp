# ops/

Operational config as code (monitoring-logging-tracing.md, slos-alerts.md).

| Dir | Contains | Blueprint |
|---|---|---|
| `dashboards/` | 7 Grafana dashboards as code + `generate.py` (rebuild) + Grafana provider | [monitoring-logging-tracing.md](../Docs/09-observability/monitoring-logging-tracing.md) §4 |
| `alerts/` | Prometheus rules: `slo-burn.yaml` (multi-window burn), `platform.yaml` (cause/ticket) | [slos-alerts.md](../Docs/09-observability/slos-alerts.md) |
| `synthetic/` | External-vantage probe CronJob (emits `synthetic_probe_success`) | monitoring §5 |
| `runbooks/` | Per-alert runbooks, DR procedures, postmortem template | [disaster-recovery.md](../Docs/08-devops/disaster-recovery.md) |
| `loadtest/` | k6 + Go WS load clients, durability auditor | [load-and-chaos-testing.md](../Docs/10-testing/load-and-chaos-testing.md) |

The deployables expose Prometheus `/metrics` (RED + SLO-bucket histograms) via
`server/internal/platform/observability`; traces export to the OTLP collector
when `WA_OTEL_ENDPOINT` is set. CI (`deploy` job) validates dashboards JSON +
alert/synthetic YAML.

Regenerate dashboards: `python3 ops/dashboards/generate.py`.
