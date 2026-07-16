# Monitoring, Logging & Tracing

| Doc | LGTM stack, OTel conventions, log schema |
|---|---|
| Status | v1.0 · Upstream: [/HLD.md](../../HLD.md) §18 · Stack: Prometheus · Grafana · Loki · Tempo · OpenTelemetry; Alertmanager → on-call |

## 1. Instrumentation conventions (every deployable)

- OTel SDK; resource attrs: `service.name`, `service.version`, `deployment.environment`.
- **Traces:** every REST/gRPC/WS frame carries W3C tracecontext; one trace spans client-send → gateway → core-api → NATS publish → recipient gateway → client-deliver (NATS headers propagate context). Tail-sampling: keep all errors + slow (>1 s) + 1% baseline.
- **Metrics:** RED per surface (rate/errors/duration) + golden domain SLIs (§slos-alerts.md); histograms with SLO-aligned buckets (e.g. msg latency: 50/100/150/250/500/1000 ms).
- **Logs:** structured JSON via slog → Loki. One log line per request/frame outcome at the outermost handler (design-patterns §5). Levels: prod = info.

## 2. Log schema (binding — enforced by shared logger package)

```json
{ "ts": "...", "level": "info", "service": "core-api", "trace_id": "...",
  "event": "msg.accept", "code": "OK", "duration_ms": 4,
  "device_id": "uuid", "conversation_id": "uuid" }
```

**Banned fields (lint on logger call sites):** message content/ciphertext, phone numbers, tokens/JWTs, key material, push tokens, IPs at info level (debug only, 14-d retention). `user_id/device_id` allowed — they're opaque UUIDs required for ops.

## 3. Domain metrics catalog (the ones dashboards are built from)

| Metric | Type | Source |
|---|---|---|
| `msg_accept_total{result}` / `msg_accept_duration` | counter/histogram | chat |
| `msg_e2e_latency` (synthetic probe measured) | histogram | probe |
| `inbox_backlog_rows` / `inbox_oldest_undelivered_online_s` | gauge | chat sweeper |
| `fanout_queue_depth` / `fanout_batch_duration` | gauge/histogram | fan-out worker |
| `ws_connections{pod,platform}` / `ws_connect_total{result}` | gauge/counter | gateway |
| `ws_replay_depth` / `ws_send_buffer_highwater_total` | gauge/counter | gateway |
| `nats_redeliveries_total{stream}` / `nats_dlq_total` | counter | consumers |
| `push_handoff_duration{provider}` / `push_breaker_state` | histogram/gauge | notification-svc |
| `call_setup_duration` / `sfu_room_packet_loss` | histogram | call-ctl + LiveKit exporter |
| `ptt_grant_duration` | histogram | ptt |
| `otp_requests_total{result}` + spend estimate | counter | auth |
| `pg_replication_lag_s`, PgBouncer pool waits | gauge | exporters |

## 4. Dashboards (ops/dashboards/, provisioned as code)

1. **Platform overview** — SLO burn-down, golden signals per deployable.
2. **Messaging pipeline** — accept→fan-out→deliver funnel, backlog age, dedupe hits.
3. **Connections** — conns by pod/platform, connect success, drain events, replay depth.
4. **Calls/PTT** — setup latency, active rooms, loss/jitter percentiles, floor grants.
5. **Data tier** — PG (tps, lag, pool waits, partition sizes), Valkey (ops, memory, hit rates), NATS (pending, redeliveries), MinIO (capacity, request rates).
6. **Abuse/security** — OTP anomalies, rate-limit hits by scope, report queue depth, admin actions.
7. **Product (metadata-only, HLD §18.1)** — DAU/MAU, signups, messages/day counts, call minutes, crash-free rate (GlitchTip).

## 5. Synthetic probes (external vantage)

Every 60 s: scripted two-client round trip (register-less probe accounts): WS connect → send → deliver → receipt; call setup to answer; media presign+upload 100 KB. Probes emit `msg_e2e_latency` etc. — **SLOs are measured from probes + real-traffic histograms, not from server-side-only latency.**

## 6. Retention

Metrics 13 mo (downsampled) · logs 14–30 d · traces 7 d (tail-sampled) — consistent with privacy posture (HLD §7.5).
