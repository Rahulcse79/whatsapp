# Non-Functional Requirements

| Doc | NFRs — binding targets |
|---|---|
| Status | v1.0 |
| Upstream | [/HLD.md](../../HLD.md) §2.2, §18 |
| Convention | NFR-`<nn>`. Each has an owner metric in [09-observability/slos-alerts.md](../09-observability/slos-alerts.md). |

## Performance & scale

| # | Requirement | Target |
|---|---|---|
| NFR-01 | Peak concurrent users | 20,000 (3× burst headroom = 60,000) |
| NFR-02 | Registered users | 200,000 |
| NFR-03 | Message ingress peak | 300 msg/s (designed ≥ 10×) |
| NFR-04 | Fan-out deliveries peak | ~1,200/s |
| NFR-05 | Message E2E latency, both online, same region | p50 ≤ 150 ms, p95 ≤ 500 ms |
| NFR-06 | Call setup | p95 ≤ 3 s |
| NFR-07 | Voice one-way latency | p95 ≤ 150 ms |
| NFR-08 | PTT floor grant | p95 ≤ 200 ms |
| NFR-09 | WS connect success | ≥ 99.9% |
| NFR-10 | Cold app open → usable chat list (mobile, mid-tier device) | ≤ 1.5 s (local-first) |

## Reliability & durability

| # | Requirement | Target |
|---|---|---|
| NFR-11 | Availability | 99.9% monthly (≈ 43 min budget); single-box offline profile: best-effort (HLD §17.5) |
| NFR-12 | Durability of server-ACKed messages | Zero loss — inbox row until every recipient device ACKs or 30 d |
| NFR-13 | Disaster recovery | RPO ≤ 5 min (PG WAL), RTO ≤ 60 min |
| NFR-14 | Deploys | Zero-downtime rolling/canary; WS draining; expand–contract migrations |
| NFR-15 | Exactly-once user experience | At-least-once transport + UUIDv7 idempotency = effectively-once |

## Security & privacy

| # | Requirement | Target |
|---|---|---|
| NFR-16 | E2EE default | All chats, calls, stories, backups; libsignal; no plaintext content paths server-side |
| NFR-17 | Transport | TLS 1.3 only; cert pinning on mobile; mTLS service-to-service |
| NFR-18 | At rest | LUKS volumes; SQLCipher on device; secrets via SOPS+age |
| NFR-19 | Metadata minimization | Retention table in HLD §7.5 is binding; no content-derived analytics |
| NFR-20 | Compliance | GDPR-style export & delete ≤ 30 days |
| NFR-21 | Abuse resistance | OTP/send-rate/enumeration controls per HLD §15.4; spend circuit-breaker on SMS |

## Client constraints

| # | Requirement | Target |
|---|---|---|
| NFR-22 | Battery | Push-driven wakeups only; no polling; heartbeat 30 s with jitter |
| NFR-23 | Data frugality | Protobuf frames; zstd > 1 KB; media compression client-side |
| NFR-24 | Offline UX | Full read + compose + search offline; queued sends |
| NFR-25 | Accessibility | WCAG 2.2 AA baseline (web + mobile) |
| NFR-26 | Localization-ready | All strings externalized; RTL support |

## Operability

| # | Requirement | Target |
|---|---|---|
| NFR-27 | Team size to operate | 2–4 engineers |
| NFR-28 | Observability | Metrics/logs/traces in one Grafana pane; every request traceable end-to-end |
| NFR-29 | Self-host/offline | Full stack on one box (Compose/K3s); air-gap profile with zero hard cloud deps |
| NFR-30 | Scale ladder | 100k/1M reachable via node counts + planned partitioning only (HLD §16.3) |
