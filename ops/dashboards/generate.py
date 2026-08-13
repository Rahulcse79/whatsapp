#!/usr/bin/env python3
"""Grafana dashboards as code (monitoring-logging-tracing.md §4).

Run `python3 ops/dashboards/generate.py` to (re)emit the 7 dashboard JSON files
in this directory. Panels are built from dicts so the output is always valid
JSON; PromQL targets reference the metrics catalog (§3). Datasource is the
Grafana-provisioned Prometheus (uid "prometheus").
"""
import json
import os

DS = {"type": "prometheus", "uid": "prometheus"}
HERE = os.path.dirname(os.path.abspath(__file__))


def panel(pid, title, expr, x, y, w=12, h=8, ptype="timeseries", unit=None, legend="__auto"):
    p = {
        "id": pid,
        "title": title,
        "type": ptype,
        "datasource": DS,
        "gridPos": {"h": h, "w": w, "x": x, "y": y},
        "fieldConfig": {"defaults": {"unit": unit} if unit else {}, "overrides": []},
        "targets": [{"refId": "A", "datasource": DS, "expr": expr, "legendFormat": legend}],
    }
    return p


def dashboard(uid, title, panels, tags=None):
    return {
        "uid": uid,
        "title": title,
        "tags": ["whatsapp-v2"] + (tags or []),
        "schemaVersion": 39,
        "version": 1,
        "editable": True,
        "refresh": "30s",
        "time": {"from": "now-6h", "to": "now"},
        "timezone": "",
        "templating": {"list": []},
        "annotations": {"list": []},
        "panels": panels,
    }


def grid(specs):
    """Lay panels out two-per-row; specs are (title, expr, opts) tuples."""
    out = []
    for i, (title, expr, opts) in enumerate(specs):
        x = 0 if i % 2 == 0 else 12
        y = (i // 2) * 8
        out.append(panel(i + 1, title, expr, x, y, **opts))
    return out


DASHBOARDS = {
    "platform-overview": ("Platform overview", [
        ("Request rate by service", "sum by (service) (rate(http_requests_total[5m]))", {}),
        ("5xx error ratio", "sum by (service) (rate(http_requests_total{status=~\"5..\"}[5m])) / sum by (service) (rate(http_requests_total[5m]))", {"unit": "percentunit"}),
        ("p95 request duration", "histogram_quantile(0.95, sum by (le, service) (rate(http_request_duration_seconds_bucket[5m])))", {"unit": "s"}),
        ("In-flight requests", "sum by (service) (http_requests_in_flight)", {}),
    ]),
    "messaging-pipeline": ("Messaging pipeline", [
        ("Accept rate by result", "sum by (result) (rate(msg_accept_total[5m]))", {}),
        ("p95 accept latency", "histogram_quantile(0.95, sum by (le) (rate(msg_accept_duration_bucket[5m])))", {"unit": "s"}),
        ("Inbox backlog rows", "sum(inbox_backlog_rows)", {}),
        ("Oldest undelivered (online)", "max(inbox_oldest_undelivered_online_s)", {"unit": "s"}),
        ("Fan-out queue depth", "sum(fanout_queue_depth)", {}),
        ("p95 fan-out batch", "histogram_quantile(0.95, sum by (le) (rate(fanout_batch_duration_bucket[5m])))", {"unit": "s"}),
    ]),
    "connections": ("Connections", [
        ("WS connections by platform", "sum by (platform) (ws_connections)", {}),
        ("Connect success ratio", "sum(rate(ws_connect_total{result=\"ok\"}[5m])) / sum(rate(ws_connect_total[5m]))", {"unit": "percentunit"}),
        ("Replay depth (p95)", "quantile(0.95, ws_replay_depth)", {}),
        ("Send-buffer high-water rate", "sum(rate(ws_send_buffer_highwater_total[5m]))", {}),
    ]),
    "calls-ptt": ("Calls / PTT", [
        ("p95 call setup", "histogram_quantile(0.95, sum by (le) (rate(call_setup_duration_bucket[5m])))", {"unit": "s"}),
        ("SFU packet loss (p95)", "quantile(0.95, sfu_room_packet_loss)", {"unit": "percentunit"}),
        ("p95 PTT floor grant", "histogram_quantile(0.95, sum by (le) (rate(ptt_grant_duration_bucket[5m])))", {"unit": "s"}),
    ]),
    "data-tier": ("Data tier", [
        ("PG replication lag", "max(pg_replication_lag_s)", {"unit": "s"}),
        ("NATS redeliveries", "sum by (stream) (rate(nats_redeliveries_total[5m]))", {}),
        ("NATS DLQ total", "sum(nats_dlq_total)", {}),
        ("Push handoff p95 by provider", "histogram_quantile(0.95, sum by (le, provider) (rate(push_handoff_duration_bucket[5m])))", {"unit": "s"}),
    ]),
    "abuse-security": ("Abuse / security", [
        ("OTP requests by result", "sum by (result) (rate(otp_requests_total[5m]))", {}),
        ("Rate-limit hits by scope", "sum by (scope) (rate(ratelimit_hits_total[5m]))", {}),
        ("Push breaker open", "max by (provider) (push_breaker_state)", {}),
        ("Report queue depth", "sum(report_queue_depth)", {}),
    ]),
    # Metadata-only product analytics (HLD §18.1). DAU/MAU come from the
    # analytics service's distinct sketch (product_dau/product_mau gauges);
    # signups is a counter; messages ride the existing chat counter; crash-free
    # is fed by client health pings (internal/platform/crash).
    "product": ("Product (metadata-only)", [
        ("Daily active users", "sum(product_dau)", {}),
        ("Monthly active users (30d)", "sum(product_mau)", {}),
        ("Signups (rate)", "sum(rate(product_signups_total[1h]))", {}),
        ("Messages per day", "sum(increase(msg_accept_total[24h]))", {}),
        ("Crash-free sessions", "avg(product_crash_free_ratio)", {"unit": "percentunit"}),
    ]),
}


def main():
    for uid, (title, specs) in DASHBOARDS.items():
        d = dashboard(uid, title, grid([(t, e, o) for (t, e, o) in specs]))
        path = os.path.join(HERE, f"{uid}.json")
        with open(path, "w") as f:
            json.dump(d, f, indent=2)
            f.write("\n")
        print("wrote", path)


if __name__ == "__main__":
    main()
