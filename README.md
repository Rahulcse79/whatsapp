# WhatsApp V2 — Production-Grade E2EE Messaging Platform

A WhatsApp-equivalent messaging platform: end-to-end-encrypted 1:1 and group chat (≤ 1,024 members), media sharing (≤ 25 MB), voice/video calls (≤ 32 participants), push-to-talk, stories, and multi-device support — sized for **20,000 peak concurrent users** with a documented scale ladder to 1M+.

| | |
|---|---|
| Status | **Planning / blueprint phase — no implementation code yet** |
| Architecture source of truth | [HLD.md](HLD.md) (v1.2) |
| Documentation index | [Docs/README.md](Docs/README.md) |
| Task sheet | [Docs/WhatsApp-V2-Tasks.xlsx](Docs/WhatsApp-V2-Tasks.xlsx) |

## Stack (decided — see HLD §1.1 and Docs/02-architecture/adr/)

**Go 1.24** · PostgreSQL 17 · Valkey 8 · **NATS JetStream** (not Kafka) · MinIO · LiveKit + coturn · libsignal · React 19 + Vite PWA (not Next.js) · React Native + Expo (not Flutter) · Envoy Gateway · Kubernetes + ArgoCD · Prometheus/Grafana/Loki/Tempo

## Monorepo layout

```
whatsapp-v2/
├── HLD.md          # canonical High-Level Design (v1.2)
├── Docs/           # complete documentation blueprint (start at Docs/README.md)
├── clients/        # mobile (React Native), web (React PWA), shared packages
├── server/         # Go backend: cmd/ (5 deployables), internal/ (bounded contexts), proto/
├── deploy/         # compose (dev), helm, argocd, terraform
└── ops/            # dashboards, alerts, runbooks, loadtest
```

## The five deployables

| Deployable | Role |
|---|---|
| `core-api` | Modular monolith: auth, users/contacts, chat, groups, call-control + PTT, stories, admin |
| `ws-gateway` | Stateless WebSocket tier (~20k conns/pod), session resume, frame routing |
| `media-svc` | Presigned uploads, quotas, GC against MinIO |
| `notification-svc` | FCM/APNs/ntfy dispatch, token lifecycle |
| `rtc` | LiveKit SFU pool + coturn (infra, host-network) |

## Golden rules

1. **E2EE bounds everything.** The server relays ciphertext; it never reads content. Any feature that requires server-side content access is wrong by construction.
2. **Relay, don't archive.** Undelivered messages live ≤ 30 days server-side; clients own history.
3. **Docs before code.** Every feature is specified in `Docs/` before implementation starts.
4. **Offline-capable by design.** The whole platform runs on a single self-hosted server (HLD §17.5).
