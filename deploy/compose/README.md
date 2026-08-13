# Dev / single-box stack

```bash
cp .env.example .env        # then edit if this box is exposed
make dev-up                 # from repo root: PG + Valkey + NATS + MinIO
make dev-up-all             # + Grafana/Prometheus/Loki + ntfy (offline profile)
```

| Service | Port | Credentials (dev defaults) |
|---|---|---|
| PostgreSQL | 5432 | whatsapp / devpassword |
| Valkey | 6379 | — |
| NATS (JetStream) | 4222 (mon: 8222) | — |
| MinIO S3 / console | 9000 / 9001 | minioadmin / minioadmin |
| ntfy (offline push) | 8090 | — |
| SMTP catcher (mailhog) | 1025 (UI 8025) | — |
| step-ca (private CA) | 9000 | — |
| Grafana | 3000 | admin / dev (anonymous viewer on) |
| Prometheus | 9090 | — |

Buckets `media`, `backups`, `wal-archive` are created automatically (minio-init).
This stack is the Profile-A/B seed from [Docs/08-devops/offline-local-server.md](../../Docs/08-devops/offline-local-server.md).

## Offline profile (HLD §17.5)

The `offline` compose profile adds the edge substitutes — self-hosted push
(ntfy), email OTP (mailhog SMTP), and a private CA (step-ca):

```
docker compose --profile offline up -d
```

Then run the Go services against the substitutes (no FCM/APNs/SMS/public CA):

```
WA_ENV=offline WA_OTP_CHANNEL=email WA_SMTP_HOST=smtp WA_SMTP_PORT=1025 \
WA_SMTP_FROM=no-reply@wa.internal WA_NTFY_URL=http://ntfy:80 ...
```

The config guard **rejects** `WA_OTP_CHANNEL=mock` in `offline`, and `email`
requires `WA_SMTP_HOST` + `WA_SMTP_FROM` — so a misconfigured offline box fails
fast rather than silently letting anyone log in. Codes land in the mailhog UI
(:8025). The Helm `values-offline.yaml` overlay wires the same for K3s.
