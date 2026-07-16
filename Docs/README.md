# Documentation Index — WhatsApp V2

| | |
|---|---|
| Status | Blueprint v1.0 — generated 2026-07-16 |
| Source of truth | [/HLD.md](../HLD.md) (v1.2). Where any document conflicts with the HLD, **the HLD wins** and the document must be fixed. |
| Reading order | Folders are numbered 00–14; read in order for onboarding. |

## How to use this blueprint with AI-assisted implementation

Each document is self-contained enough to drive one implementation task. The workflow: pick a task from [12-planning/task-breakdown.md](12-planning/task-breakdown.md) → read the linked docs → implement **one feature only** → test → merge. Never ask an AI (or a human) to build a whole service in one pass.

## 00 — Product

| Doc | Contents |
|---|---|
| [product-vision.md](00-product/product-vision.md) | Vision, goals, success metrics, target users |
| [scope-and-competitors.md](00-product/scope-and-competitors.md) | V2/V3 scope boundaries, competitor analysis |

## 01 — Requirements

| Doc | Contents |
|---|---|
| [functional-requirements.md](01-requirements/functional-requirements.md) | Every FR, numbered, with E2EE adjustments |
| [non-functional-requirements.md](01-requirements/non-functional-requirements.md) | NFRs: latency, availability, durability, privacy |
| [user-stories-personas.md](01-requirements/user-stories-personas.md) | Personas, user stories, acceptance criteria conventions |

## 02 — Architecture

| Doc | Contents |
|---|---|
| [system-architecture.md](02-architecture/system-architecture.md) | Condensed system view; context diagram; principles |
| [microservices.md](02-architecture/microservices.md) | Bounded contexts → 5 deployables; split triggers |
| [sequence-diagrams.md](02-architecture/sequence-diagrams.md) | All core flows end-to-end |
| [data-structures-algorithms.md](02-architecture/data-structures-algorithms.md) | The performance-critical DS&A choices |
| [design-patterns-error-handling.md](02-architecture/design-patterns-error-handling.md) | Patterns, error taxonomy, idempotency, retries |
| [adr/](02-architecture/adr/README.md) | Architecture Decision Records (Go, NATS, LiveKit, relay model…) |

## 03 — Database

| Doc | Contents |
|---|---|
| [database-design.md](03-database/database-design.md) | Full PostgreSQL schema, ERD, partitioning, indexes |
| [valkey-keyspace.md](03-database/valkey-keyspace.md) | Every key family, TTLs, atomicity contracts |
| [backup-recovery.md](03-database/backup-recovery.md) | WAL-G, PITR, restore drills |

## 04 — API

| Doc | Contents |
|---|---|
| [api-standards.md](04-api/api-standards.md) | Conventions, versioning, errors, rate limits, pagination |
| [auth-users-api.md](04-api/auth-users-api.md) | Auth, devices, keys, users, contacts REST |
| [messaging-groups-api.md](04-api/messaging-groups-api.md) | Groups REST + message semantics |
| [media-stories-api.md](04-api/media-stories-api.md) | Upload/download, stories REST |
| [calls-ptt-api.md](04-api/calls-ptt-api.md) | Call control + PTT REST/WS surface |
| [websocket-protocol.md](04-api/websocket-protocol.md) | The protobuf WS frame protocol (the heart of the system) |
| [internal-events-nats.md](04-api/internal-events-nats.md) | NATS subjects, streams, consumers, delivery contracts |

## 05 — Services (LLD per deployable)

| Doc | Contents |
|---|---|
| [core-api-lld.md](05-services/core-api-lld.md) | Modular monolith internals, module interfaces |
| [ws-gateway-lld.md](05-services/ws-gateway-lld.md) | Connection registry, resume, backpressure, drain |
| [media-svc-lld.md](05-services/media-svc-lld.md) | Upload orchestration, quotas, GC |
| [notification-svc-lld.md](05-services/notification-svc-lld.md) | Dispatch pipeline, provider drivers, ntfy |
| [rtc-lld.md](05-services/rtc-lld.md) | call-ctl, LiveKit topology, PTT floor control |

## 06 — Security

| Doc | Contents |
|---|---|
| [security-architecture.md](06-security/security-architecture.md) | AuthN/Z, transport, at-rest, secrets, admin console |
| [e2ee-design.md](06-security/e2ee-design.md) | libsignal: X3DH, Double Ratchet, Sender Keys, verification |
| [threat-model-abuse.md](06-security/threat-model-abuse.md) | STRIDE-style threat model + anti-abuse controls |

## 07 — Scalability & Performance

| Doc | Contents |
|---|---|
| [capacity-planning.md](07-scalability/capacity-planning.md) | Back-of-envelope math, hardware estimate |
| [scalability-strategy.md](07-scalability/scalability-strategy.md) | Vertical/horizontal plans, 20k → 100k → 1M ladder |
| [performance-optimization.md](07-scalability/performance-optimization.md) | Optimization checklist + bottleneck register |

## 08 — DevOps

| Doc | Contents |
|---|---|
| [ci-cd.md](08-devops/ci-cd.md) | Pipeline stages, gates, rollback |
| [kubernetes-deployment.md](08-devops/kubernetes-deployment.md) | Cluster topology, environments, zero-downtime |
| [offline-local-server.md](08-devops/offline-local-server.md) | Self-hosted/air-gapped profile (HLD §17.5 expanded) |
| [disaster-recovery.md](08-devops/disaster-recovery.md) | RPO/RTO, backup matrix, game days |

## 09 — Observability

| Doc | Contents |
|---|---|
| [monitoring-logging-tracing.md](09-observability/monitoring-logging-tracing.md) | LGTM stack, OTel conventions, log schema |
| [slos-alerts.md](09-observability/slos-alerts.md) | SLIs/SLOs, burn-rate alerts, synthetic probes |

## 10 — Testing

| Doc | Contents |
|---|---|
| [test-strategy.md](10-testing/test-strategy.md) | Test pyramid, protocol tests, coverage policy |
| [load-and-chaos-testing.md](10-testing/load-and-chaos-testing.md) | 20k-concurrent load profile, chaos scenarios |

## 11 — Clients

| Doc | Contents |
|---|---|
| [mobile-app-architecture.md](11-clients/mobile-app-architecture.md) | React Native + Expo: modules, calls, push |
| [web-app-architecture.md](11-clients/web-app-architecture.md) | React + Vite PWA: state, workers, install |
| [offline-sync-local-store.md](11-clients/offline-sync-local-store.md) | SQLCipher schema, outbox, delta sync, conflicts |

## 12 — Planning

| Doc | Contents |
|---|---|
| [roadmap-milestones.md](12-planning/roadmap-milestones.md) | Phases P0–P4, exit criteria, V3 backlog |
| [epics.md](12-planning/epics.md) | 30 epics with features, stories, phase mapping |
| [task-breakdown.md](12-planning/task-breakdown.md) | Implementation tasks in dependency order |

## 13 — Standards

| Doc | Contents |
|---|---|
| [coding-standards.md](13-standards/coding-standards.md) | Go + TypeScript standards, lint gates |
| [git-workflow.md](13-standards/git-workflow.md) | Trunk-based flow, commits, reviews, releases |

## 14 — AI (V3)

| Doc | Contents |
|---|---|
| [ai-features-v3.md](14-ai/ai-features-v3.md) | On-device AI under E2EE: smart replies, transcription |

## Decided — do not re-litigate without an ADR superseding it

| Decision | Where |
|---|---|
| Go backend (not Java/Spring) | [ADR-002](02-architecture/adr/ADR-002-go-backend.md), HLD §1.1 |
| NATS JetStream (not Kafka) | [ADR-003](02-architecture/adr/ADR-003-nats-over-kafka.md), HLD correction #3 |
| Relay model — no server archive | [ADR-001](02-architecture/adr/ADR-001-relay-model.md), HLD correction #5 |
| LiveKit SFU (operate, don't build) | [ADR-004](02-architecture/adr/ADR-004-livekit-sfu.md), HLD §10.1 |
| Client-side content search (no OpenSearch) | [ADR-005](02-architecture/adr/ADR-005-client-side-search.md), HLD correction #4 |
| React Native + Vite PWA (not Flutter/Next.js) | [ADR-006](02-architecture/adr/ADR-006-client-stack.md), HLD §1.1 |
