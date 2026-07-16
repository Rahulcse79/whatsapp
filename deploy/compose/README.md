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
| Grafana | 3000 | admin / dev (anonymous viewer on) |
| Prometheus | 9090 | — |

Buckets `media`, `backups`, `wal-archive` are created automatically (minio-init).
This stack is the Profile-A/B seed from [Docs/08-devops/offline-local-server.md](../../Docs/08-devops/offline-local-server.md);
step-ca and Gitea join it at tasks T0.22/T0.04.
