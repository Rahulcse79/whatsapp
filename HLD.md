# WhatsApp V2 — High-Level Design (HLD)

| Field | Value |
|---|---|
| Document | High-Level Design — WhatsApp V2 messaging platform |
| Status | Draft v1.2 — for review |
| Date | 2026-07-16 (v1.0: 2026-07-14) |
| v1.2 changes | **Offline / self-hosted local-server deployment profile** (§17.5): every cloud-touching edge gets a self-hosted substitute (UnifiedPush/ntfy, email-OTP/TOTP, step-ca private CA, Gitea CI, local registry, MetalLB, chrony); single-box sizing; honest limits (iOS push & SMS cannot be self-hosted). |
| v1.1 changes | Reviewed the proposed 30-epic roadmap and alternate stack (Java/Spring, Kafka, OpenSearch, Flutter). **Go backend reaffirmed** (§1.1). Scoped in: admin console (§15.6), privacy-preserving analytics (§18.1), client-side link previews, GIFs/stickers, contact favorites, mute/badge, accessibility (§2.1). Deferred to V3: channels/communities, AI features (§2.3). Full epic disposition: Appendix D. |
| Scope | Architecture and planning only. No implementation code in this document. |
| Sizing target | **20,000 peak concurrent users** (~200,000 registered), designed with 3× burst headroom and a documented scale ladder to 100k → 1M+ concurrent |
| Reviewer stance | Written as a principal-architect review and correction of the raw V2/V3 requirements prompt |

---

## 1. Architecture Review — Corrections to the Raw Design

These are the deliberate corrections made to the raw requirements. Each one is a real design decision, not a style preference.

1. **Right-sized the scale target.** The raw doc asks for "10M+ users / 1M concurrent" while the actual requirement is 20k users. Designing day-one for 1M concurrent multiplies cost and operational complexity ~10× for zero benefit. This HLD sizes every tier for 20k concurrent, and §16.3 shows exactly what changes (and only what changes) at 100k and 1M. Scaling later is adding nodes, not re-architecting.
2. **Cut the datastore zoo.** MongoDB and Cassandra are removed. PostgreSQL + Valkey cover every access pattern at this scale with huge headroom (§3). Every extra datastore adds a backup strategy, an upgrade path, a failure mode, and a security surface. Cassandra/ScyllaDB re-enters the picture only at ~1M concurrent, for exactly one table (§16.3).
3. **Kafka → NATS JetStream.** Kafka's operational weight (broker fleet, partition management, rebalancing storms, JVM tuning) is not justified below tens of thousands of messages/second. NATS JetStream provides durable streams and at-least-once delivery in three tiny pods. Kafka remains the documented swap-in at extreme scale.
4. **Server-side message search is impossible with E2EE — the raw design demands both.** The server only ever holds ciphertext, so it cannot index message content. Corrected: message/media search runs client-side over the local database (SQLite FTS5). Server-side search exists only for non-E2EE metadata: usernames, group names/descriptions (§14).
5. **Messages are relayed, not archived.** The server stores only *encrypted, not-yet-delivered* messages with a 30-day TTL — WhatsApp's actual production model. Clients own chat history; optional backups are client-encrypted blobs the server cannot read. This cuts server storage ~100×, shrinks the breach blast radius, and is the correct privacy posture (§7.2).
6. **Server-side thumbnail generation and media scanning are also impossible under E2EE.** Media is encrypted client-side before upload. Corrected: compression, thumbnail/blurhash generation happen on the sender's device; the tiny encrypted thumbnail travels inside the message envelope (§9).
7. **Next.js dropped for the chat client.** A chat app is a long-lived, stateful, offline-capable client; SSR adds nothing. Web = React + Vite PWA. Mobile = React Native (Expo). Next.js only if a public marketing site is ever needed.
8. **Don't build an SFU — operate one.** LiveKit is selected over raw WebRTC mesh, mediasoup, Janus, and Jitsi (full comparison in §10.1). Mesh calling collapses beyond ~4 participants; building a production SFU service on mediasoup is 6+ months of media-server engineering that LiveKit already ships with mobile SDKs, simulcast, E2EE support, and recording egress.
9. **Call recording conflicts with E2EE.** Server-side recording of an E2EE call is a contradiction. Corrected: recording, if kept, is client-side with mandatory consent signaling to all participants; any server-side recording requires visibly downgrading the room to transport-only encryption with an on-screen banner (§10.6).
10. **Microservice count minimized.** The raw scope implies 15+ services. We define **8 bounded contexts** but ship **5 deployables** (modular-monolith core + WS gateway + media + RTC + notifications). At 20k users, fewer moving parts = higher reliability and a team that can actually operate what it built. Contexts split into services only when load or team boundaries force it (§4.3).
11. **Redis → Valkey.** After Redis' 2024 relicensing, Valkey (Linux Foundation fork, BSD-3, drop-in compatible) is the safer pure-OSS default.
12. **ELK → Grafana LGTM stack.** Loki + Grafana + Tempo + Prometheus is dramatically lighter to operate than Elasticsearch for logs at this scale, and unifies metrics/logs/traces in one UI (§18).
13. **"Zero downtime" made concrete.** The committed SLO is **99.9% monthly availability** with rolling deploys, WebSocket connection draining, and expand–contract database migrations. Promising four nines with a small team and single-region infra would be dishonest (§17.2, §18).

### 1.1 v1.1 addendum — review of the proposed 30-epic roadmap & alternate stack (2026-07-16)

A 30-epic program breakdown and an alternate stack recommendation (Java 21 + Spring Boot, Kafka, OpenSearch, Flutter, Socket.IO, 20–25 microservices, 80–120 tables) were reviewed against this HLD. The **epic breakdown is good program management and is adopted as the planning skeleton** — the full epic-by-epic disposition is in **Appendix D**. The **stack recommendation is largely rejected** — verdicts below.

| Proposal | Verdict | Rationale |
|---|---|---|
| **Java 21 + Spring Boot backend** | **Rejected — Go 1.24 stands** | The core workload is protocol plumbing: 20k+ long-lived WebSockets, fan-out, backpressure. Goroutines cost ~4–8 KB per connection vs the JVM's per-thread/Netty overhead — one Go gateway pod holds all 20k connections in ~1.3 GB (§3). Static ~15 MB distroless images vs 200+ MB JVM images; millisecond cold starts matter for HPA burst scaling. NATS, LiveKit, coturn, and the entire K8s/Prometheus estate are Go-native — one profiling toolchain (pprof) spans our code and our infra's. No JVM heap-sizing/GC-tuning on-call burden, consistent with the small-team philosophy (correction #10). Spring's genuine strengths — enterprise integration, JPA/CRUD ecosystem — buy nothing in a relay server that mostly moves ciphertext it cannot read. |
| Spring Cloud Gateway | Moot | Falls with Spring. Envoy Gateway stands (§6). |
| Kafka | Rejected | Correction #3 — NATS JetStream. Kafka remains the documented swap-in at the ~1M-concurrent tier (§16.3). |
| OpenSearch | Rejected | Correction #4 — the server holds only ciphertext; **there is nothing to index**. Client-side SQLite FTS5 for content; PostgreSQL trigram/FTS for the metadata the server legitimately holds. An OpenSearch cluster here would be an empty, expensive liability. |
| Socket.IO / STOMP | Rejected | Raw WSS + protobuf frames (§8.1). Socket.IO's value — fallback transports, JSON room semantics — solves 2015 problems and fights a protobuf-first binary protocol. gRPC stays internal-only. |
| Flutter | Not adopted (viable, not wrong) | React Native + Expo stands (§6): one JS/TS talent pool across web + mobile, mature CallKit/ConnectionService integrations. Flutter would also work — this is a team-shape decision, not a correction. |
| 15–25 microservices, 80–120 tables | Rejected | Correction #10 — 8 bounded contexts, **5 deployables**, ~20 entities. The relay model (correction #5) deletes the message-archive tables that inflate such counts. |
| PostgreSQL · Valkey · MinIO→S3 · K8s · Prometheus/Grafana/Loki · LiveKit · React + TypeScript | Aligned | Already this HLD's choices. |

**Scope accepted from the epic roadmap** (previously missing or under-specified here): admin console & trust-and-safety tooling (§15.6), privacy-preserving product/crash analytics (§18.1), client-side link previews (§2.1), GIFs & sticker packs (§2.1), contact favorites / invite links / user QR (§2.1), per-chat mute & badge counts (§2.1), accessibility baseline (§2.1). **Deferred to V3 with architectural notes:** broadcast channels/communities and AI features (§2.3).

---

## 2. Scope & Requirements

### 2.1 Functional scope (condensed)

| Domain | In scope (V2) | E2EE-driven adjustment |
|---|---|---|
| Auth & identity | Phone + OTP signup, optional email, profile (username, photo, about), multi-device (1 primary + up to 4 linked), device management, session revocation, 2FA registration PIN | Each device has its own cryptographic identity |
| Contacts | Hashed address-book sync, in-app user search, favorite contacts, invite-a-friend share links, per-user QR code for adding contacts, block/unblock | Discovery runs on hashed phone numbers (§15.4); the server never stores a plaintext address book |
| 1:1 chat | Real-time messaging; states: sending → sent → delivered → read; typing; online/last-seen; reply, forward, copy; delete-for-me / delete-for-everyone (48 h window); edit (15 min window); pin; star; emoji reactions; link previews | Edits/deletes are overlay events; server validates sender + window only, never content. Link previews are generated on the **sender's** device (the server cannot fetch a URL it cannot see) and travel inside the E2EE envelope |
| Group chat | Up to 1,024 members; multi-admin; promote/demote; invite link + QR; permissions (who can post/edit info); announcements mode; @mentions; description; photo; pinned messages | Sender-Keys encryption: encrypt once, fan out ciphertext |
| Media | ≤ 25 MB per file; images, video, audio, voice notes, PDF/ZIP/Office docs; GIFs & sticker packs; multi-file send; previews & in-chat playback; download; progress; **resumable** uploads; client-side compression & thumbnails | All blobs encrypted client-side; server stores ciphertext only. GIF search is proxied through media-svc so client IPs never reach the GIF provider (Signal model) |
| Voice calls | 1:1 and group (≤ 32); history; missed-call handling; mute, speaker, Bluetooth; low-bandwidth mode (Opus DTX/FEC) | E2EE via SFrame/insertable streams |
| Video calls | 1:1 and group (≤ 32); camera switch; screen share; background blur (on-device); PiP; adaptive quality via simulcast; network recovery (ICE restart) | Same E2EE model |
| PTT (walkie-talkie) | One speaker at a time; press-and-hold floor acquisition; server-authoritative floor control; speaker display; FIFO request queue; ≤ 200 ms floor-grant latency; audio-only rooms up to 500 listeners | Floor control is metadata (server-side); audio is E2EE |
| Status / stories | Photo, video, text; 24 h expiry; audience privacy controls; view counts; reactions | Story content E2E-encrypted with a per-story key distributed to eligible contacts |
| Notifications | Push (FCM/APNs), silent/data-only, mentions, calls (VoIP push), previews, inline actions, per-chat & global mute, notification settings, badge counts | Push payloads never contain plaintext — client wakes, fetches, decrypts, renders locally. Unread/badge counts are computed on-device post-decryption |
| Search | Users & groups (server, metadata); messages/media/files (client-side FTS over local DB) | See correction #4 |
| UX | Dark/light themes, responsive web + mobile + tablet, desktop via PWA (Tauri shell later), offline-first with sync, WCAG 2.2 AA accessibility baseline | Local-first architecture (§4.1) |

### 2.2 Non-functional requirements

| NFR | Target |
|---|---|
| Peak concurrent users | 20,000 (design headroom 3× = 60,000 burst) |
| Registered users | 200,000 |
| Message throughput | 300 msg/s ingress peak; ~1,200 deliveries/s after group fan-out (designed capacity ≥ 10×) |
| Message latency (both online, same region) | p50 ≤ 150 ms, p95 ≤ 500 ms |
| Call setup time | p95 ≤ 3 s |
| Voice one-way latency | p95 ≤ 150 ms |
| PTT floor grant | p95 ≤ 200 ms |
| Availability | 99.9% monthly (≈ 43 min error budget) |
| Durability | Zero loss of server-ACKed messages |
| Disaster recovery | RPO ≤ 5 min, RTO ≤ 60 min |
| Security | E2EE by default for all chats, calls, stories; TLS 1.3 everywhere; encrypted at rest |
| Privacy/compliance | GDPR-style account export & delete; minimal metadata retention (§7.5) |
| Mobile constraints | Push-driven wakeups (no polling); battery- and data-frugal protocols |

### 2.3 Explicit non-goals for V2

- Federation/interop (Matrix-style), business/bots API, payments.
- Multi-region active-active (single region + DR site; ladder in §16.3).
- Server-side message archive or server-readable analytics on content.
- Native desktop apps (PWA first; Tauri wrapper is a V3 item).
- **Broadcast channels / communities** (WhatsApp Channels-style, Epic 7 of the proposed roadmap). Architecturally a different product: one-to-many read fan-out over a follower graph, deliberately **not** E2EE-pairwise. Bolting it onto the chat relay would distort the inbox model — V3, designed as its own bounded context with its own store.
- **AI features** (smart replies, transcription, translation, AI search — Epic 28). Under E2EE these must run **on-device** or on explicitly user-disclosed content; server-side AI over messages is impossible by design. V3 backlog (Appendix D).

### 2.4 Sizing assumptions

- "20k users" is interpreted as **20k peak concurrent** (the harder case). If it means 20k *registered total*, this design is ~10× over-provisioned — shrink node counts in §22, change nothing architecturally.
- DAU ≈ 60,000 (30% of registered); average 40 messages sent/DAU/day; 12% of messages carry media (avg 350 KB after client compression); 6% of concurrent users in calls at peak.

---

## 3. Capacity Estimation (back-of-envelope)

| Dimension | Math | Result | Verdict |
|---|---|---|---|
| Messages/day | 60k DAU × 40 | 2.4 M/day (~28/s avg) | Trivial |
| Peak ingress | ×8 peak factor | ~250–300 msg/s | One core-api pod handles this |
| Peak fan-out | ×4 avg recipients (groups incl.) | ~1,200 deliveries/s | NATS capacity ~10⁶/s per node |
| Inbox writes (offline recipients ~60%) | 1,200 × 0.6 | ~700 batched rows/s | < 10% of one NVMe Postgres box |
| DB size (relay model) | Undelivered buffer + metadata | ≤ ~50 GB steady state | See correction #5 |
| Media ingest | 288k files/day × 350 KB | ~100 GB/day | MinIO, easy |
| Media storage (30-day server TTL) | 100 GB × 30 | ~3 TB rolling | 4-node MinIO EC pool |
| Media egress | ~2.5 downloads/file | ~250 GB/day, ~200 Mbps peak | Fine on 1–10 Gbps |
| WebSocket connections | 20k × ~64 KB buffers | ~1.3 GB RAM total | One Go pod could hold all 20k; we run 3 for HA |
| Presence ops | 20k heartbeats / 30 s | ~700 ops/s Valkey | Capacity ~150k ops/s |
| Concurrent call media | 960 audio @ 40 kbps + 240 video @ ~1.2 Mbps ×2.5 subs | < 1 Gbps SFU egress | 2 × LiveKit nodes with headroom |
| Push notifications | ~2 M/day, peak ~100/s | Negligible | FCM/APNs are free |

**Conclusion:** every tier has ≥ 10× headroom on modest hardware. This is what justifies 5 deployables and a small cluster instead of a 15-service constellation.

---

## 4. Architecture Overview

### 4.1 Guiding principles

1. **Local-first clients, dumb-relay server.** E2EE forces the server into a relay role; embrace it. Clients hold history, do search, render previews. This also gives the best offline UX for free.
2. **Stateless service tier.** All durable state lives in PostgreSQL, Valkey, NATS, MinIO. Any pod can die at any moment; users reconnect anywhere.
3. **At-least-once + idempotency = effectively-once.** Every client mutation carries a client-generated UUIDv7; the server dedupes. Retries are always safe.
4. **One writer of truth per data type.** No dual writes across stores; derived data (caches, counters) is rebuildable.
5. **Boring technology, few moving parts.** Chosen components are the most-operated OSS in their category as of 2026.
6. **Right-size now, leave seams.** Bounded contexts have hard internal interfaces so extraction to separate services is a deployment change, not a rewrite.

### 4.2 System context diagram

```
                        ┌──────────────────────────────────────────────────┐
                        │                     CLIENTS                      │
                        │   React Native (iOS / Android)  ·  React PWA     │
                        │   local-first: SQLCipher DB · libsignal ratchet  │
                        │   client-side media compress + encrypt + thumbs  │
                        └────────┬──────────────┬───────────────┬──────────┘
                                 │              │               │
                          HTTPS (REST)    WSS (protobuf)   WebRTC (SRTP/E2EE)
                                 │              │               │
              ┌──────────────────▼──────────────▼─────┐   ┌─────▼──────────────┐
              │   EDGE — Envoy Gateway (TLS 1.3, WAF  │   │  coturn            │
              │   rules, global rate limits, L4/L7 LB)│   │  (STUN / TURN)     │
              └──────┬─────────────────────┬──────────┘   └─────┬──────────────┘
                     │                     │                    │
          ┌──────────▼──────────┐ ┌────────▼─────────┐ ┌────────▼──────────────┐
          │      core-api       │ │    ws-gateway    │ │    LiveKit SFU pool   │
          │  (modular monolith) │ │ (Go · stateless  │ │  voice / video / PTT  │
          │  auth · users ·     │◄┤  ~20k conns/pod) │ │  media plane · egress │
          │  chat · groups ·    │ └────────┬─────────┘ └────────┬──────────────┘
          │  call-ctl · ptt ·   │          │                    │ webhooks/API
          │  stories            │          │                    │
          └───┬──────────┬──────┘          │           ┌────────▼──────────────┐
              │          │                 │           │  call-ctl (in core)   │
     ┌────────▼───┐ ┌────▼─────────────────▼────────┐  │  rooms · tokens · PTT │
     │ media-svc  │ │     NATS JetStream (×3)       │  └───────────────────────┘
     │ presign ·  │ │ live fan-out · durable events │──────► notification-svc ──► FCM / APNs
     │ quotas · GC│ │ · push dispatch queue         │
     └────┬───────┘ └───────┬───────────────────────┘
          │                 │
 ┌────────▼─────────┐ ┌─────▼───────────────┐ ┌──────────────────────┐
 │ MinIO (S3, EC)   │ │ PostgreSQL 17       │ │ Valkey (HA/Sentinel) │
 │ encrypted media  │ │ primary + replica   │ │ presence · dedupe ·  │
 │ encrypted backups│ │ inbox · meta · keys │ │ PTT floor · routing  │
 └──────────────────┘ └─────────────────────┘ └──────────────────────┘

 Cross-cutting: Prometheus · Grafana · Loki · Tempo · OpenTelemetry  |  ArgoCD + Argo Rollouts (GitOps)
```

### 4.3 Deployment-unit strategy (monolith-first, service-ready)

| Deployable | Contains (bounded contexts) | Why grouped |
|---|---|---|
| `core-api` | auth, users/contacts, chat, groups, call-control + PTT floor, stories | Shared data model, low individual load; one binary = simple ops. Contexts are internal modules with hard interfaces. |
| `ws-gateway` | connection handling, frame routing, session resume | Different scaling axis (connections, not CPU); isolates reconnect storms from business logic |
| `media-svc` | upload orchestration, presigned URLs, quotas, garbage collection | Isolates large-payload traffic and MinIO coupling |
| `notification-svc` | FCM/APNs dispatch, token lifecycle, retries | External-dependency isolation (push provider outages must not back-pressure chat) |
| `rtc` (infra) | LiveKit SFU pool + coturn | Media plane; host-network, bandwidth-bound nodes |

**Split triggers (documented in advance):** extract a context from `core-api` when (a) its load profile diverges ≥ 5× from siblings, (b) it needs independent release cadence, or (c) a dedicated team owns it. Likely first extraction at ~100k concurrent: `chat` and `presence`.

---

## 5. Bounded Contexts & Responsibilities

| Context | Owns | Primary data | Scale notes |
|---|---|---|---|
| Auth & Identity | OTP flow, JWT issuance/rotation, devices, 2FA PIN, sessions | `users`, `devices`, `otp_attempts` | CPU-light; rate-limit heavy |
| Keys | Signal prekey bundles: identity keys, signed prekeys, one-time prekeys | `prekeys`, `signed_prekeys` | Read burst on new-session setup |
| Chat | Message accept/dedupe/sequence, inbox fan-out, receipts, overlay events (edit/delete/react) | `message_inbox`, `conversations` | The hot path; see §8 |
| Groups | Membership, roles, permissions, invite links/QR, group metadata | `groups`, `group_members`, `invite_links` | Fan-out multiplier for chat |
| Media | Upload sessions, presigned URLs, quota, TTL GC, story blobs | `media_objects` + MinIO | Bandwidth-bound |
| Calls & PTT | Room lifecycle, join tokens, ring/busy state machine, call history, PTT floor control | `call_records`, Valkey floor keys | Latency-critical control plane |
| Presence | Online/last-seen, typing relays, subscriptions | Valkey only (ephemeral) | Never touches Postgres |
| Notifications | Push token registry, dispatch, provider failover | `push_tokens` | External-API bound |
| Admin & T&S *(v1.1)* | Moderation queue (user-consented reports), account actions (warn/suspend/ban), feature-flag management, metadata-only analytics rollups | `reports`, `audit_log`, `feature_flags` | Low volume; ships inside core-api; separate admin SPA gated by SSO + IP allowlist (§15.6) |

---

## 6. Technology Stack & Decisions

| Layer | Choice (2026-current) | Why | Rejected & why |
|---|---|---|---|
| Backend language | **Go 1.24+** | Best-in-class for high-connection-count network services; single static binary; tiny containers; mainstream hiring pool | Java/Spring (heavier runtime, slower cold paths); Node (weaker CPU-bound perf, weaker typing for protocol code); Elixir (excellent fit but niche hiring) |
| Client (mobile) | **React Native + Expo** | One team ships iOS+Android; mature call/push integrations (CallKit/ConnectionService); OTA updates | Flutter (viable; RN chosen for JS talent overlap with web) |
| Client (web/desktop) | **React 19 + Vite PWA**, TanStack Query, Zustand; Tauri shell in V3 | Chat = stateful SPA; PWA gives offline + install | Next.js — SSR pointless for an authed realtime app (correction #7) |
| Client local DB | **SQLite (SQLCipher) + FTS5**; OP-SQLite on RN | Local-first history, encrypted at rest, full-text search on device | — |
| Wire format | **Protobuf** over WSS and internal gRPC | Compact, schema-evolvable, codegen for Go/TS | JSON (2–5× larger frames, no schema) |
| Relational DB | **PostgreSQL 17** (CloudNativePG operator; PgBouncer) | Handles all workloads here with 10× headroom; best operational tooling in OSS | MongoDB/Cassandra — no access pattern requires them at this scale (correction #2) |
| Cache / ephemeral | **Valkey 8** (Sentinel HA → Cluster later) | Presence, dedupe, routing table, PTT floor, rate limits; BSD-3 license | Redis (license risk, correction #10) |
| Messaging backbone | **NATS JetStream 2.11** (3-node RAFT) | Durable at-least-once streams, per-device subjects, ~ms latency, trivial ops | Kafka (ops weight, correction #3); RabbitMQ (weaker horizontal story) |
| Object storage | **MinIO** (4-node erasure coding) | S3 API, self-hosted, presigned URLs, ILM/TTL policies | Cloud S3 acceptable swap-in if not self-hosting |
| RTC engine | **LiveKit** (self-hosted) + **coturn** | See §10.1 comparison | mediasoup/Janus/Jitsi/mesh — §10.1 |
| E2EE | **libsignal** (official Rust core + bindings): X3DH + Double Ratchet; Sender Keys for groups | Never roll your own crypto; audited, battle-tested | Custom crypto (absolutely not); MLS (promising for groups, revisit V3) |
| Edge / ingress | **Envoy Gateway** (TLS 1.3, HTTP/3 for media paths), WAF rules, global rate limits | Modern, K8s-native, gRPC/WS first-class | Nginx (fine, weaker gRPC/WS ergonomics) |
| Orchestration | **Kubernetes 1.33** (3 small control + workers; K3s acceptable) | Zero-downtime rollouts, HPA, operator ecosystem | Plain Docker Compose (no zero-downtime story beyond dev) |
| GitOps / CD | **ArgoCD + Argo Rollouts** (canary) | Declarative, auditable, instant rollback | Script-based deploys |
| Observability | **Prometheus + Grafana + Loki + Tempo + OpenTelemetry** | One pane; light footprint | ELK (correction #12) |
| Secrets | **SOPS + age** in Git; Vault when team grows | Right-sized secret management | — |
| Push | FCM (Android/web) + APNs (iOS, PushKit for calls); **pluggable push interface** so the offline profile swaps in UnifiedPush/ntfy (§17.5) | Only options that wake devices reliably over the public internet | — |
| SMS OTP | Pluggable provider (Twilio / MSG91 / AWS SNS) behind an interface; offline profile swaps in email OTP / TOTP (§17.5) | SMS is inherently paid; abstract it, cap the spend (§15.5) | — |

---

## 7. Data Architecture

### 7.1 Server data model (entities, not DDL)

| Entity | Purpose | Key fields (conceptual) | Notes |
|---|---|---|---|
| `users` | Identity & profile | id (UUIDv7), phone_hash, username, avatar_ref, about, privacy_settings, 2fa_pin_hash | Phone stored hashed+peppered for lookup; plaintext phone encrypted separately for SMS |
| `devices` | Multi-device registry | id, user_id, platform, signed device cert, last_active, push_token_ref | 1 primary + ≤ 4 linked; revocation kills sessions + prekeys |
| `prekeys` / `signed_prekeys` | Signal key bundles (public only) | device_id, key material (public), one-time flags | Server never holds private keys |
| `conversations` | 1:1 and group conversation registry | id, type, per-conversation sequence counter | Sequence gives total order per conversation |
| `message_inbox` | **Ciphertext relay buffer** | recipient_device_id, conversation_id, seq, sender, ciphertext blob, expires_at | Row deleted on device ACK; TTL 30 days; partitioned by (hash(device), month) |
| `groups`, `group_members`, `invite_links` | Group metadata | roles, permissions bitmap, link tokens + QR secret, revocation | Metadata is server-visible by necessity; content never is |
| `media_objects` | Blob registry | object_key, size, content-hash, refcount, expires_at, uploader quota accounting | Blob itself in MinIO, always ciphertext |
| `stories`, `story_views` | Status posts + view receipts | media ref, audience snapshot, expires_at (24 h) | Hard-deleted by TTL job |
| `call_records` | Call history (metadata only) | participants, direction, start/end, outcome (missed/declined/completed) | 90-day retention |
| `push_tokens` | FCM/APNs tokens per device | token, provider, liveness | Rotated on provider feedback |
| `contacts`, `blocks` | Address book sync (hashed), block list | hashed phone matching | See §15.4 enumeration note |
| `otp_attempts`, `rate_counters` | Abuse control | sliding windows | Short TTL |
| `reports` | User reports for trust & safety | reporter, target, reason, *forwarded ciphertext with reporter's consent* | Only user-disclosed content, WhatsApp-style |
| `audit_log` | Immutable record of every admin/moderation action | actor, action, target, reason, timestamp | Append-only; admins cannot delete it (§15.6) |
| `feature_flags` | Server-evaluated release flags | flag, targeting rules, rollout % | Managed via admin console; consumed by the flag SDK (§17.2) |

**What is deliberately absent:** a plaintext `messages` table, server-side chat archive, server-readable media. (Correction #5.)

### 7.2 The relay model

- Server persists a message **only** until every recipient device ACKs it (or 30 days, whichever first).
- Clients are the system of record for history, stars, pins, search.
- Optional **encrypted backups**: client encrypts its history export with a key derived from a user passphrase (Argon2id) or a 64-digit recovery key, uploads to MinIO. Server cannot read it; losing the key loses the backup — surfaced clearly in UX.

### 7.3 Client data model (local-first)

SQLCipher database per device: `chats`, `messages` (+ FTS5 index), `attachments`, `contacts`, `signal_sessions/keys`, `outbox` (pending sends with retry state), `settings`. Multi-device history bootstrap = encrypted transfer from primary device (QR-authenticated), or backup restore.

### 7.4 Valkey usage map

| Key family | Purpose | TTL |
|---|---|---|
| `route:{device_id}` | Which ws-gateway pod holds the device's connection | Heartbeat-refreshed, 90 s |
| `presence:{user_id}` | Online flag + last-seen | 60 s sliding |
| `dedupe:{msg_uuid}` | Idempotency for sends | 24 h |
| `typing:{conversation}` | Ephemeral typing fan-out | 5 s |
| `ptt:{room}:floor` / `ptt:{room}:queue` | Floor token (with fencing seq) + FIFO wait queue | 2 s heartbeat / room life |
| `rl:*` | Rate-limit sliding windows | Window-sized |

### 7.5 Retention policy

| Data | Retention |
|---|---|
| OTP codes | 10 minutes |
| Undelivered messages (ciphertext) | ≤ 30 days |
| Media blobs (server) | 30 days after last reference, then GC |
| Stories | 24 hours hard delete |
| Call records | 90 days |
| Push tokens | Until provider invalidates or device revoked |
| Logs/traces | 14–30 days |
| Deleted account | Purged ≤ 30 days, immediate tombstone |

---

## 8. Real-Time Messaging Design

### 8.1 Connection layer

- **WSS + Protobuf frames**, one multiplexed connection per device.
- ws-gateway is **stateless**: on connect it authenticates (short-lived JWT), registers `route:{device}` in Valkey, and subscribes to the device's NATS subject. Any pod can serve any device — **no sticky sessions needed**; the routing table is the source of truth.
- **Session resume:** reconnecting clients present a resume token + last-received inbox cursor; the gateway replays from the durable inbox. Design invariant: reconnect never loses an ACKed-to-server message.
- Heartbeats every 30 s with jitter; server-initiated `drain` hints during deploys (§17.2).

### 8.2 Message lifecycle (1:1) — sequence

```
Sender app          ws-gateway        chat (core-api)      NATS / inbox        Recipient
   │ 1. encrypt per     │                   │                   │                  │
   │    recipient device│                   │                   │                  │
   │ 2. send frame ────►│ 3. authz, forward►│                   │                  │
   │    (UUIDv7 id)     │                   │ 4. dedupe (Valkey)│                  │
   │                    │                   │ 5. assign conv seq│                  │
   │                    │                   │ 6. write inbox row(s), publish ────► │
   │ ◄─── 7. ACK "sent" ┤◄──────────────────┤                   │ 8. deliver ────► │
   │                    │                   │                   │ ◄─ 9. delivery   │
   │ ◄── 10. "delivered" receipt ───────────┤ (delete inbox row)│    receipt       │
   │ ◄── 11. "read" receipt (batched, privacy-gated) ───────────┴──────────────────│
```

- UI states: **sending** (local outbox) → **sent** (server ACK) → **delivered** (device receipt) → **read** (chat opened; suppressed if user disabled read receipts).
- If no recipient device is online at step 8 → notification-svc sends a **data-only push**; message waits in the durable inbox.
- Writes are batched; receipts are coalesced (≤ 1 flush/250 ms per conversation) to cut chatter.

### 8.3 Group fan-out

- Sender encrypts **once** with its group Sender Key → server fans the single ciphertext to N member devices (inbox rows + live pushes). 1,024-member groups are a write-amplification hot spot: sends to huge groups are rate-limited per sender, and fan-out happens async off the ACK path (sender sees "sent" after durable accept, not after N writes).
- Membership changes trigger Sender Key rotation (forward secrecy on remove).
- Receipts aggregate: "delivered/read" ticks flip when **all** members reach the state; per-member detail available in message info.

### 8.4 Overlay events

Edit (≤ 15 min), delete-for-everyone (≤ 48 h), reactions, and pins are **events referencing the original message UUID**, encrypted like any message. Server validates only sender identity and time window (it cannot see content); clients apply the overlay to local history.

### 8.5 Presence, typing, last-seen

- Ephemeral only — never persisted to Postgres.
- Presence updates fan out **on subscription**: a client subscribes to presence only for the ~30 chats visible on screen — this avoids O(contacts) broadcast storms.
- Typing indicators throttle to 1 event / 3 s and expire via TTL.
- Privacy settings (last-seen audience, online visibility) enforced server-side at subscription time.

---

## 9. Media Pipeline

Upload sequence (correction #6 applies — all content processing is client-side):

1. Client compresses (images → WebP/AVIF ≈ q80; video → H.264/AAC ≤ 720p; voice → Opus), generates thumbnail + blurhash.
2. Client encrypts the file with a random per-file key (AES-256-GCM, chunked).
3. `POST /v1/media/uploads` → media-svc validates type allowlist, ≤ 25 MB, per-user quota & rate → returns **S3 multipart presigned URLs**.
4. Client uploads chunks **directly to MinIO** (parallel; resume = re-request URLs for missing parts only — this is the resumable-upload story).
5. Client confirms; media-svc verifies size + content hash, registers `media_objects`.
6. The message envelope (E2EE) carries: object key, file key, hash, encrypted mini-thumbnail. Recipients download via presigned GET, verify hash, decrypt locally.
7. GC job: refcount/TTL sweep after 30 days (clients have long since stored their copy).

Multi-file sends = N parallel upload sessions under one message. Videos play from local store after download; progressive playback via ranged GETs on ciphertext chunks is a V3 enhancement.

---

## 10. Voice & Video Calling

### 10.1 RTC engine comparison

| Option | Type | Strengths | Why not / when |
|---|---|---|---|
| Raw WebRTC mesh | P2P full-mesh | Zero server media cost; lowest latency for 1:1 | Upload bandwidth explodes at N>3–4; no group story; still needs TURN. Used implicitly *via* SFU fallback only. |
| **LiveKit** ✅ | SFU platform (Go) | Horizontal SFU pool, simulcast + adaptive subscriptions, official RN/iOS/Android/JS SDKs, E2EE (insertable streams), screen share, egress/recording, active 2026 community, self-host friendly | Chosen. Operate it, don't build it. |
| mediasoup | SFU **library** (C++/Node) | Excellent performance, fine-grained control | You must build signaling, scaling, recording, SDK glue — months of media-plane engineering for no product gain at 20k users |
| Janus | SFU server (C) + plugins | Mature, flexible | Plugin architecture + C ops experience needed; SDK story weaker than LiveKit |
| Jitsi (JVB) | Full meeting product | Fastest path to "a meetings app" | Hard to embed deeply into a custom chat UX; XMPP/Prosody stack drag; meeting-shaped, not chat-shaped |
| MCU (mixing) | Server mixes streams | Thin clients | CPU cost per room is brutal; kills E2EE (server must decrypt). Rejected outright. |

**Decision: SFU architecture on LiveKit** for 1:1, group voice, group video, screen share, and PTT audio; coturn for STUN/TURN (~10–20% of calls need TURN relay).

### 10.2 Topology

- LiveKit pool on dedicated host-network nodes; a room lives on one node (node-count scales rooms; single-room scale-out via cascading is a >32-participant concern we cap away).
- `call-ctl` (in core-api) owns the control plane: room creation, short-lived join JWTs, ring state machine, history. LiveKit webhooks feed room lifecycle back.

### 10.3 1:1 call flow

1. Caller taps call → call-ctl creates room, issues join tokens.
2. Callee devices get WS `call.offer` **plus** VoIP push (APNs PushKit → CallKit; FCM high-priority → ConnectionService) so locked phones ring.
3. Ring state machine: ringing → answered / declined / busy / timeout (45 s → "missed call" + notification).
4. Both join via SFU; ICE negotiates (STUN first, TURN fallback); media = SRTP with **E2EE frame encryption** keyed from the participants' Signal session.
5. Network change → ICE restart; target recovery < 5 s. Downgrade path: video → audio-only on sustained loss.
6. End → call_record persisted (metadata only).

### 10.4 Group calls (≤ 32)

Same control plane; simulcast (3 spatial layers) + active-speaker detection; SFU forwards the layer matching each subscriber's downlink. Screen share is an extra video track with content-optimized encoding. Background blur runs **on-device** (MediaPipe-class model) — never server-side (E2EE).

### 10.5 Quality & low-bandwidth mode

Opus with DTX + in-band FEC (voice survives ~30% loss); adaptive bitrate 6–32 kbps voice; video floor 90p@100 kbps before auto-drop to audio; TURN over TCP/443 as last resort for hostile networks.

### 10.6 Recording (corrected)

Client-side recording with mandatory in-room consent signal to all participants. Server-side recording via LiveKit egress exists **only** for rooms explicitly created as "transport-encrypted" (E2EE off) with a persistent on-screen banner — an org/enterprise feature, off by default.

---

## 11. Push-to-Talk (PTT)

### 11.1 Design: server-authoritative floor control over a standing SFU room

- PTT room = LiveKit **audio-only** room; all participants keep a **pre-negotiated, server-muted** microphone track. This is the trick that makes floor grants feel instant — no renegotiation on grant.
- **Floor token** in Valkey: `ptt:{room}:floor` = {device, fencing_seq, expiry}; acquired atomically (single Lua/script step). Fencing sequence prevents a zombie ex-speaker from injecting audio after a network partition.
- **Queue**: FIFO sorted-set of pending requests with per-user position feedback.
- Speaker heartbeats every 500 ms; 2 missed beats → auto-release → grant to next in queue.
- Anti-hogging: configurable max speak duration (default 60 s) → forced release.
- Server enforces at the media plane too: publish permission is granted/revoked via LiveKit server API, so a malicious client cannot talk without the floor.

### 11.2 Floor state machine

```
                 press-and-hold (request)
   IDLE ─────────────────────────────────► REQUESTED (queued, position shown)
    ▲                                          │
    │            cancel / timeout              │ floor granted (token + fencing seq)
    │◄─────────────────────────────────────────┤
    │                                          ▼
    │                                      SPEAKING ── release / max-duration /
    │                                          │        heartbeat lapse
    │◄─────────────────────────────────────────┘
              floor passes to next queued requester; all clients render active speaker
```

### 11.3 Latency budget (target ≤ 200 ms press-to-audio)

| Step | Budget |
|---|---|
| WS request → Valkey atomic acquire | ~30 ms |
| Grant event to speaker + permission flip at SFU | ~40 ms |
| Client unmute pre-negotiated track → first RTP at listeners | ~80–100 ms |
| **Total** | **~150–170 ms** ✅ |

Scale: 1 speaker → N listeners is the cheapest possible SFU workload (~40 kbps × N); a 500-listener room ≈ 20 Mbps on one node. Very large broadcast rooms (5k+) would move listeners to a streaming tier (V3).

---

## 12. Stories / Status

- Reuses the media pipeline; content encrypted with a **per-story key** distributed via existing pairwise Signal sessions to the audience snapshot (privacy setting evaluated at post time).
- Server stores ciphertext with 24 h TTL; hard-delete job + MinIO lifecycle rule as backstop.
- View receipts and reactions are lightweight E2EE events back to the author; view counts aggregate client-side.

## 13. Notifications

- **Never any plaintext in a push** (correction of a common leak): pushes are data-only wake signals; the device fetches ciphertext, decrypts locally, renders the notification (iOS Notification Service Extension; Android foreground fetch).
- Calls: PushKit/CallKit (iOS) and high-priority FCM/ConnectionService (Android) for lock-screen ringing.
- Mention notifications: the *client* detects @mentions post-decryption and elevates priority locally.
- notification-svc consumes a NATS dispatch stream, handles provider retries/backoff and token invalidation feedback; provider outage back-pressures only this stream, never chat.

## 14. Search

| Scope | Where | How |
|---|---|---|
| Messages, media, files, in-chat history | **Client-side** | SQLite FTS5 over the local decrypted store (device-encrypted at rest). E2EE makes server-side content search impossible — see correction #4. |
| Users, groups (names/descriptions), public directory | Server | PostgreSQL trigram + FTS on metadata the server legitimately holds. No OpenSearch needed at this scale. |
| Multi-device consistency | Each device indexes its own synced history | — |

---

## 15. Security Architecture

### 15.1 End-to-end encryption

- **Pairwise:** libsignal — X3DH key agreement + Double Ratchet (forward secrecy + post-compromise security). Per-device sessions; server stores only public prekey bundles.
- **Groups:** Sender Keys (encrypt once, fan out); key rotation on membership change. MLS is the V3 watch item for very large groups.
- **Media:** per-file random keys inside the E2EE envelope (§9).
- **Calls:** DTLS-SRTP transport + SFrame-style frame encryption via insertable streams, keyed from Signal sessions — the SFU forwards packets it cannot read.
- **Verification:** safety numbers / QR per contact; key-change warnings; device list signed by the primary device's identity key so linked devices can't be silently injected.

### 15.2 Authentication & authorization

- Phone OTP registration (rate-limited hard, §15.5) with optional email fallback; **2FA registration PIN** (Argon2id-hashed) to block SIM-swap re-registration.
- Short-lived access JWTs (10 min) + rotating refresh tokens bound to device identity; refresh reuse detection → session kill.
- Device management: list, rename, revoke; revocation invalidates tokens, prekeys, and push routes atomically.
- Service-to-service: mTLS inside the cluster; least-privilege DB roles per deployable.

### 15.3 Transport & at-rest

TLS 1.3 only (+ certificate pinning in mobile apps); HSTS; encrypted disks (LUKS) under Postgres/MinIO/Valkey; secrets via SOPS-age → Vault later; images distroless, non-root, cosign-signed with SBOM.

### 15.4 Anti-abuse & rate limiting

| Vector | Control |
|---|---|
| OTP pumping / SMS toll fraud | Per-number 3/hr & 5/day; per-IP and per-ASN caps; device attestation (Play Integrity / App Attest); spend circuit-breaker on the SMS provider |
| Spam messaging | Per-device send-rate ceilings; new-account graduated limits; big-group send throttles; metadata-only heuristics (fan-out patterns) — content is invisible by design |
| Enumeration via contact sync | Hashed-number discovery with peppering + strict rate limits; private-set-intersection is the V3 upgrade |
| Abuse reporting | User-consented forwarding of the reported ciphertext (WhatsApp model) → trust & safety queue |
| DDoS | Edge rate limits + WAF, SYN-cookie LB, per-connection frame quotas, autoscaling gateways, jittered reconnect backoff |

### 15.5 Threat model highlights

| Threat | Mitigation |
|---|---|
| Full server compromise | Attacker gets ciphertext + metadata only (relay model); keys live on devices |
| MITM | TLS 1.3 + pinning; safety-number verification catches key substitution |
| Stolen phone / SIM swap | 2FA PIN; remote device revocation; re-registration alerts to all devices |
| Malicious linked device | Primary-signed device list; visible device roster; per-device revocation |
| Push-channel snooping | Data-only pushes; zero content ever transits FCM/APNs |
| Replay / duplication | Ratchet nonces + server dedupe + per-conversation sequences |
| Media URL leakage | Short-TTL presigned URLs; blobs are ciphertext anyway |

### 15.6 Admin console & trust-and-safety tooling *(v1.1)*

The epic roadmap correctly calls for an admin dashboard; this HLD scopes it deliberately narrowly, because E2EE bounds what an admin can ever see.

- **Surface:** a separate React SPA on an internal route, gated by SSO (OIDC) + IP allowlist + hardware-key 2FA. It talks to the Admin & T&S module inside core-api — no separate service at this scale.
- **Admins can:** search users by username/phone-hash; view metadata (registration date, device count, report history); action reports (dismiss / warn / suspend / ban); manage feature flags and server config; view the aggregate dashboards of §18.1.
- **Admins cannot, by construction:** read any message, view any media, or see who talks to whom beyond what a specific user-consented report discloses. There is no "God mode" — the data does not exist server-side (correction #5).
- **Every admin action** writes an append-only `audit_log` row; audit review is part of the quarterly security review.
- **RBAC:** viewer → T&S agent → operator → owner, least privilege by default.

---

## 16. Scaling Strategy — Vertical AND Horizontal

### 16.1 Vertical scaling plan (scale-up ceilings per component)

| Component | Start size | Sensible max (scale-up) | What vertical buys |
|---|---|---|---|
| ws-gateway pod | 2 vCPU / 4 GB (~20k conns) | 16 vCPU / 32 GB (~250k conns) | Prefer horizontal past ~50k conns/pod — blast radius of one pod dying matters more than density |
| core-api pod | 4 vCPU / 8 GB | 32 vCPU / 64 GB | Pure headroom; stateless so horizontal is equally easy |
| PostgreSQL | 8 vCPU / 32 GB / NVMe | 96 vCPU / 768 GB / NVMe RAID | **The big vertical lever**: one primary comfortably rides from 20k → ~1M concurrent workload (with partitioning + PgBouncer) before sharding is even discussed |
| Valkey | 2 vCPU / 8 GB | 8 vCPU / 64 GB | Core is single-threaded (~150k ops/s); vertical helps memory, not throughput → cluster for throughput |
| NATS node | 2 vCPU / 4 GB | 16 vCPU | ~1M msg/s per node before horizontal matters |
| LiveKit node | 8 vCPU / 16 GB | 32 vCPU / 10 GbE | ~thousands of audio / ~1–2k video streams per node |
| MinIO | 4 × (4 vCPU / 4 TB) | Bigger disks/NICs | Horizontal-native; vertical = disk & NIC only |

### 16.2 Horizontal scaling plan (scale-out per component)

| Component | How it scales out | Coordination cost |
|---|---|---|
| ws-gateway | Add pods behind L4 least-conn LB; **no stickiness** — Valkey routing table + per-device NATS subjects mean any pod serves anyone | None (that's the point) |
| core-api / media / notify | Stateless → HPA on CPU + queue depth | None |
| PostgreSQL | Read replicas for read paths (profiles, groups, keys); `message_inbox` partitioned by (device hash, month); logical shard by user_id only at extreme scale | Replica lag monitoring; shard router at 1M tier |
| Valkey | Sentinel HA now → Valkey Cluster (slot-sharded) when ops/s demands | Client cluster awareness |
| NATS | 3-node RAFT now → add nodes/leafnodes per region | Minimal |
| LiveKit | Pool of nodes; rooms distribute across nodes; regional pools later | Room-to-node registry (built in) |
| MinIO | Add erasure-set server pools | Rebalancing window |
| coturn | Multiple instances, DNS/anycast selection | None |

### 16.3 Scale ladder — what changes and when

| Tier | 20k concurrent (this design) | 100k concurrent | 1M concurrent |
|---|---|---|---|
| Gateways | 3 pods | ~10 pods (same design) | Pods across AZs; regional gateway pools |
| core-api | 2–3 pods | Extract `chat` + `presence` into own services | Cell-based architecture (users sharded into self-contained cells) |
| PostgreSQL | 1 primary + 1 replica | Bigger box (vertical) + 2 replicas + aggressive partitioning | Shard inbox by user_id **or** move inbox table to ScyllaDB; everything else stays PG |
| Valkey | Sentinel (3 small) | Cluster, 3 shards | Cluster per cell |
| NATS | 3 nodes | 5 nodes | Per-region clusters + leafnodes (or Kafka swap if event archival demanded) |
| LiveKit | 2 nodes | 5–8 nodes + regional pool | Multi-region pools, geo-routed by call locality |
| MinIO | 4 nodes (EC 2+2) | 8 nodes + CDN in front of media GETs | Multi-site replication + CDN |
| Region model | Single region + DR backups | Single primary region + read-close media CDN | Multi-region active-active for gateways/media; single-home per user for messaging state |
| Team implication | 2–4 engineers can run this | +SRE on-call rotation | Dedicated platform team |

**The invariant:** no tier requires a redesign to climb the ladder — only more nodes, partitioning that is planned from day one, and (at 1M) one datastore swap for one table.

---

## 17. Deployment Architecture

### 17.1 Topology

```
                    Kubernetes cluster (single region, 3 AZs where available)
 ┌────────────────────────────────────────────────────────────────────────────┐
 │  node pool: general (3× 8vCPU/16GB)     node pool: rtc (2× 16vCPU, host-   │
 │   · envoy gateway  · core-api ×2-3       network, high egress)             │
 │   · ws-gateway ×3  · media-svc ×2         · livekit ×2   · coturn          │
 │   · notification   · NATS ×3                                               │
 │                                                                            │
 │  node pool: data (dedicated)             observability (1× 8vCPU/32GB)     │
 │   · PostgreSQL primary + replica          · prometheus · grafana           │
 │     (CloudNativePG, anti-affinity)        · loki · tempo                   │
 │   · Valkey + sentinels                                                     │
 │   · MinIO ×4 (EC 2+2, 4TB each)                                            │
 └────────────────────────────────────────────────────────────────────────────┘
   GitOps: ArgoCD (app-of-apps) · Argo Rollouts (canary) · cert-manager · external-dns
   Off-site: encrypted PG WAL archive + MinIO backup bucket in a second location
```

### 17.2 Zero-downtime practices

- **Rolling + canary** (Argo Rollouts: 10% → 50% → 100% gated on error-rate/latency metrics).
- **WebSocket draining:** SIGTERM → gateway stops accepting, sends `reconnect` hints jittered over 60–120 s; session-resume protocol guarantees no message loss across the hop.
- **Expand–contract DB migrations:** additive change → deploy code reading both → backfill → contract. Never a breaking migration in one step. Migration job runs before rollout proceeds.
- Feature flags (Unleash/OpenFeature) decouple deploy from release.

### 17.3 Environments

`dev` (Docker Compose / kind, full stack on a laptop) → `staging` (scaled-down K8s, real FCM/APNs sandbox, synthetic load) → `prod`. Same Helm charts, different value overlays; config drift impossible by construction (GitOps). The offline/local-server profile (§17.5) is a fourth overlay on the **same** charts — never a fork of the stack.

### 17.4 Disaster recovery

| Item | Approach | Objective |
|---|---|---|
| PostgreSQL | Continuous WAL archiving (WAL-G) to off-site bucket + nightly base backup; PITR | RPO ≤ 5 min |
| MinIO | Versioning + scheduled sync of backup/media buckets to second site | RPO ≤ 24 h (media), ≤ 1 h (backups) |
| Valkey | Rebuildable (presence/cache) — no backup needed; nothing durable lives here | — |
| NATS streams | Short-retention transit; source-of-truth is PG inbox → replayable | — |
| Whole cluster | Terraform + GitOps = full cluster rebuild from repo | RTO ≤ 60 min |
| Drills | Quarterly restore-from-backup and region-loss game days; a backup that isn't restore-tested doesn't exist | — |

### 17.5 Offline / self-hosted local-server deployment profile *(v1.2)*

**Design rule:** the platform must run entirely on user-owned hardware with **zero hard cloud dependencies**. Every core component was already self-hosted OSS by design (PG, Valkey, NATS, MinIO, LiveKit, coturn, LGTM stack) — only the *edges* touch third-party clouds. This profile substitutes each edge and states plainly what cannot be replicated offline.

Two variants, same charts, different value overlays:

- **Profile A — local server, internet available** (home lab / on-prem): self-host everything, optionally keep FCM/APNs and SMS as the only outbound calls.
- **Profile B — fully air-gapped LAN** (no internet, ever): every substitution below is mandatory.

| Cloud-touching edge | Default (v1.1) | Offline substitute | Notes |
|---|---|---|---|
| Push wakeups (Android/web) | FCM | **UnifiedPush via self-hosted ntfy** (or Gotify); Android app keeps a lightweight persistent connection to the local ntfy; web PWA uses standard Web Push against a self-hosted push endpoint | Behind the same pluggable push interface (§6); notification-svc gains an ntfy driver |
| Push wakeups (iOS) | APNs (PushKit) | **None exists.** APNs is the only channel that wakes a locked iPhone — Apple allows no substitute | Hard limit; see below |
| SMS OTP | Twilio/MSG91/SNS | **Email OTP** via self-hosted SMTP (Stalwart/Postfix) and/or **TOTP** enrollment; admin-provisioned accounts for closed deployments; optional GSM-modem gateway (gammu-smsd) if phone-number identity must be kept | Identity anchor becomes username/email instead of phone number |
| Device attestation | Play Integrity / App Attest | Disabled; compensate with graduated rate limits + admin account approval | Both are Google/Apple cloud APIs |
| GIF search proxy | Giphy/Tenor via media-svc | Disabled; local sticker packs served from MinIO | Content pack curated by the operator |
| TLS certificates | cert-manager + Let's Encrypt | **step-ca private CA** (ACME-compatible, so cert-manager config barely changes); CA root pre-installed on enrolled devices; mobile cert pinning pins the private CA | Air-gap standard practice |
| DNS | external-dns + public DNS | CoreDNS/dnsmasq authoritative for the local zone | |
| Load balancer | Managed L4 | **MetalLB** (K8s) or HAProxy + keepalived on bare metal | |
| CI/CD + registry | GitHub Actions + public registries | **Gitea** (git + Gitea Actions) or Woodpecker; **Harbor** (or registry:2) with air-gap-mirrored base images; ArgoCD points at local Gitea | GitOps model unchanged |
| Time sync | Public NTP | Local **chrony** server | Not optional: JWT expiry, TOTP, and TLS all break on clock drift |
| Crash/product analytics | Self-hosted Sentry/GlitchTip (§18.1) | Already offline-capable | No change |
| App distribution | Play Store / App Store | Web: PWA served from the local web server. Android: signed APK sideload or a local F-Droid repo. iOS: **requires Apple infra** (TestFlight/enterprise signing, periodic online check-ins) | Second hard limit |
| STUN/TURN | coturn (already self-hosted) | coturn on the LAN; on a flat LAN most calls connect via direct host candidates and barely need it | Keep it for multi-subnet/VPN topologies |

**Hard limits — stated honestly, not designed around:**

1. **iOS on an air-gapped network degrades.** No APNs means no lock-screen message wake and no CallKit ring while locked. iOS clients receive messages/calls only while the app is foregrounded (or via background-fetch windows). Android has no such limit — UnifiedPush plus an optional foreground service gives full offline parity. If offline operation is the priority, **Android + PWA are the first-class clients**; iOS is best-effort.
2. **Phone-number identity requires the phone network.** Air-gapped identity shifts to username/email + TOTP (the auth context already abstracts the OTP channel, so this is a driver swap, not a redesign).
3. **Single box = no HA.** The 99.9% SLO applies to the multi-node profile only; a one-server deployment is best-effort availability with nightly backups to a second disk/machine (WAL-G + MinIO mirror still apply, targets relaxed).

**Single-server sizing (replaces the 14-machine estimate of §22 for this profile):**

| Tier | Hardware (one box) | Runs | Realistic capacity |
|---|---|---|---|
| Pilot / dev | 8 vCPU / 32 GB / 1 TB NVMe | Docker Compose profile from `deploy/compose/` | ~500–2,000 concurrent; all features incl. calls |
| Production single-box | 16–32 vCPU / 64–128 GB / 2× NVMe (mirror) / 10 GbE | K3s single node, full Helm stack, MinIO single-node multi-drive | ~5,000–20,000 concurrent chat; calls bounded by NIC egress (~500–1,000 concurrent call legs) |
| Two-box upgrade | + second box | Moves PG replica + backups + MinIO mirror off-box | Removes the worst single-points first |

The architecture is unchanged in every profile — same binaries, same charts, same protobuf contracts. Offline capability is a **values file**, not a fork.

---

## 18. Observability

- **OpenTelemetry SDKs** in every deployable → Prometheus (metrics), Loki (structured logs), Tempo (traces, tail-sampled), Grafana (single pane). Alertmanager → on-call.
- **Golden signals per deployable** + domain SLIs:

| SLI | SLO / alert |
|---|---|
| API availability | 99.9% monthly (alert on burn rate) |
| Message E2E latency (online↔online) | p95 ≤ 500 ms |
| WS connect success rate | ≥ 99.9% |
| Inbox backlog depth / age | Alert if oldest undelivered-to-online > 60 s |
| NATS redelivery rate | Alert on sustained growth |
| PG replication lag | Alert > 10 s |
| SFU packet loss / jitter per room | Alert p95 loss > 5% |
| PTT floor-grant latency | p95 ≤ 200 ms |
| OTP failure/spend rate | Alert on anomaly (fraud signal) |
| Push handoff latency | p95 ≤ 2 s |

- Synthetic probes: scripted two-client message round-trip + call setup every minute from an external vantage point.

### 18.1 Product & crash analytics — privacy-preserving *(v1.1)*

The roadmap's analytics epic is in scope **in metadata form only** — the server cannot analyze content it cannot read:

- **Product metrics:** DAU/MAU/retention, signups, messages relayed per day (counts only), delivery-latency percentiles, call minutes & quality aggregates, feature adoption (flag exposure). A small NATS consumer aggregates events into daily PostgreSQL rollup tables, rendered in Grafana next to the ops dashboards. No third-party analytics SDKs, no per-user behavioral profiles, no content-derived signals.
- **Crash reporting:** self-hosted Sentry (or GlitchTip) for clients and server; payloads scrubbed of PII at the SDK layer before leaving the device.

## 19. CI/CD Pipeline

| Stage | Contents |
|---|---|
| PR gates | Lint (golangci-lint, eslint, buf for protobuf), unit tests, SAST (semgrep, gosec), dependency audit, protobuf breaking-change check |
| Main build | Integration tests (Testcontainers: PG, Valkey, NATS, MinIO), E2E protocol tests (two headless clients through a real gateway), build distroless images, SBOM (syft), scan (trivy), sign (cosign), push registry |
| CD | ArgoCD auto-sync dev → staging; staging soak + synthetic load; prod via Argo Rollouts canary with metric gates |
| Rollback | Instant image rollback (GitOps revert); DB contract phases deferred ≥ 1 release so rollbacks never fight migrations |
| Cadence | Trunk-based development, deploy on green main, small batches |

---

## 20. Bottlenecks & Mitigations

| # | Bottleneck | Symptom | Mitigation |
|---|---|---|---|
| 1 | Inbox write amplification on 1,024-member groups | PG write spikes | Async fan-out off ACK path; batched inserts; per-sender big-group rate limit; inbox partitioning |
| 2 | Reconnect storm (gateway deploy/crash, mobile network flap) | Auth + resume stampede | Jittered exponential backoff baked into clients; resume tokens skip full auth; edge rate limits; multiple small gateway pods |
| 3 | Presence fan-out O(contacts) | Valkey/WS chatter | Subscription model (§8.5) — presence only for on-screen chats |
| 4 | OTP SMS abuse & cost | Fraud spend | §15.4 controls + provider spend circuit-breaker |
| 5 | Single LiveKit node per room | Big-room ceiling | Cap calls at 32; PTT rooms are 1-publisher (cheap); streaming tier for broadcast in V3 |
| 6 | MinIO egress on viral media | Bandwidth saturation | Presigned direct-to-storage (bypasses services); per-object rate caps; CDN at the 100k tier |
| 7 | NATS consumer backlog after gateway loss | Delivery lag | Durable consumers with redelivery; inbox in PG is the safety net — NATS is transit, not truth |
| 8 | Postgres connection exhaustion | Latency cliffs | PgBouncer transaction pooling from day one; per-deployable pool budgets |
| 9 | Push provider outage | Silent notification loss | Dispatch queue with retry/backoff; provider health metrics; message still waits in inbox regardless |
| 10 | Hot conversation (celebrity group) | Per-conversation seq contention | Per-conversation sequencing shards naturally; monitor; worst case dedicated lane |

## 21. Performance Optimization Checklist

- Protobuf frames (not JSON); app-level zstd only for frames > 1 KB.
- UUIDv7 everywhere (index locality vs UUIDv4's random-write pain).
- Batched inbox writes, coalesced receipts, Valkey pipelining, prepared statements, covering indexes on `(device, seq)`.
- PgBouncer transaction pooling; replicas take read-heavy endpoints (key bundles, profiles).
- HTTP/3 (QUIC) on media presigned paths — biggest win on lossy mobile networks.
- Opus DTX + FEC; simulcast; AV1 only where hardware encode exists (battery).
- Client: chat-list virtualization, lazy media hydration, delta sync by cursor, optimistic UI with outbox.
- Images AVIF/WebP; blurhash placeholders for instant perceived load.

---

## 22. Infrastructure Estimate (20k concurrent tier)

| Role | Spec | Count |
|---|---|---|
| K8s general workers | 8 vCPU / 16 GB | 3 |
| RTC nodes (LiveKit + coturn) | 16 vCPU / 32 GB, high egress | 2 |
| PostgreSQL (dedicated) | 8 vCPU / 32 GB / 1 TB NVMe | 2 (primary + replica) |
| MinIO | 4 vCPU / 8 GB / 4 TB | 4 (EC 2+2 ≈ 8 TB usable) |
| Observability | 8 vCPU / 32 GB | 1 |
| Load balancer | Managed L4 | 2 (HA) |
| **Total** | **~14 machines** | |

| Cost scenario | Estimate |
|---|---|
| EU commodity cloud (Hetzner/OVH class) | ~€700–1,000 / month |
| AWS/GCP equivalent (EKS + managed swaps) | ~$3,500–5,000 / month |
| SMS OTP (variable) | ~$0.005–0.03 per OTP — the controls in §15.4 are also cost controls |
| FCM / APNs | Free |

Managed-service swap-ins if preferred: RDS ↔ self-hosted PG, S3 ↔ MinIO, ElastiCache ↔ Valkey — the architecture is identical either way.

## 23. Delivery Roadmap

| Phase | Duration | Scope | Exit criteria |
|---|---|---|---|
| P0 — Foundations | 6–8 wks | Infra (K8s, GitOps, observability), auth + devices + keys, 1:1 E2EE chat, delivery states, presence/typing, push | Two phones exchange E2EE messages through prod-shaped infra; chaos-kill a gateway with zero message loss |
| P1 — Groups & media | 4–6 wks | Groups (Sender Keys), media pipeline, reactions/edit/delete, contact sync | 1,024-member group + 25 MB resumable media pass load test |
| P2 — Calling | 4–6 wks | LiveKit + coturn, 1:1 voice/video, CallKit/ConnectionService, group calls ≤ 32, screen share | p95 call setup ≤ 3 s; locked-phone ringing on both platforms |
| P3 — Multi-device, PTT, stories | 4–6 wks | Linked devices + history bootstrap, PTT floor control, stories, encrypted backups | PTT p95 grant ≤ 200 ms with 200 listeners; device link/revoke flows |
| P4 — Hardening & launch | 3–4 wks | Admin console + T&S tooling (§15.6), analytics rollups (§18.1), 20k-concurrent load test (3× burst), security audit + external pentest, DR restore drill, runbooks | SLOs green for 2 consecutive weeks under synthetic load |
| V3 backlog | — | Tauri desktop, MLS for mega-groups, PSI contact discovery, broadcast channels/communities, broadcast/streaming PTT tier, CDN media, progressive video playback, on-device AI (smart replies, transcription, translation), multi-region | — |

## 24. Risks & Open Questions

| Risk / question | Note |
|---|---|
| "20k users" = concurrent or total? | Designed for concurrent (harder). Confirm — affects only node counts (§2.4). |
| libsignal license (AGPL) | Fine for a service; confirm distribution/legal posture early. Alternative: implement Signal-spec via permissive libs — more effort, more risk. |
| SMS dependency | Paid + fraud-prone. Budget owner and provider choice needed in P0. |
| E2EE UX cost | Lost recovery key = lost backup; key-change warnings confuse users. Invest in UX copy early. |
| iOS review constraints | VoIP push/CallKit rules change; keep the notification fallback path tested. |
| Small-team ops of self-hosted RTC | LiveKit is the easiest self-host option, but media plane on-call is real; consider LiveKit Cloud as a paid escape hatch. |
| Data residency / jurisdiction | Not specified; single-region choice should follow the user base's legal requirements. |
| Offline profile: iOS reach | No APNs on an air-gapped LAN → iOS cannot ring/wake when locked (§17.5). If offline-first is the deployment reality, prioritize Android + PWA clients and treat iOS as best-effort. |

---

## Appendix A — Monorepo Layout (planning view)

```
whatsapp-v2/
├── clients/
│   ├── mobile/            # React Native (Expo): iOS + Android
│   ├── web/               # React + Vite PWA
│   └── packages/          # shared: proto-generated types, crypto wrapper, UI kit, sync engine
├── server/
│   ├── cmd/               # one main per deployable: core-api, ws-gateway, media-svc, notification-svc
│   ├── internal/          # bounded contexts: auth/ users/ keys/ chat/ groups/ media/ calls/ ptt/ stories/ notify/ presence/
│   └── proto/             # single source of truth: WS frames, REST/gRPC contracts, NATS event schemas
├── deploy/
│   ├── compose/           # dev: full stack on a laptop
│   ├── helm/              # charts per deployable + umbrella
│   ├── argocd/            # app-of-apps, env overlays (dev/staging/prod)
│   └── terraform/         # cluster, DNS, buckets, secrets bootstrap
├── ops/
│   ├── dashboards/  alerts/  runbooks/  loadtest/
└── docs/
    ├── HLD.md             # this document
    ├── requirements/      # functional & non-functional requirements, user stories, acceptance criteria
    ├── planning/          # roadmap, milestones, epics/ (Appendix D is the seed), task breakdown
    ├── adr/               # architecture decision records (start at ADR-001: relay model)
    ├── api/               # generated API reference
    └── testing/           # test strategy: unit, integration, load, security, chaos, UAT
```

## Appendix B — API Surface Summary (contracts, not schemas)

**REST v1 (HTTPS):**
`auth`: request-otp · verify-otp · refresh · logout · 2fa-pin — `devices`: list · link (QR) · revoke — `keys`: publish prekeys · fetch bundle(user) — `users`: profile CRUD · privacy settings · block/unblock — `contacts`: hashed sync — `groups`: create · update · members · roles · invite-links · join(link/QR) — `media`: create-upload · complete · presign-download — `stories`: post · audience · viewers — `calls`: history — `push`: register-token — `reports`: submit — `account`: export · delete

**WebSocket frames (protobuf, bidirectional):**
`hello / resume(cursor)` · `msg.send / ack / receipt(delivered|read)` · `msg.edit / delete / react / pin` · `typing` · `presence.sub / update` · `group.event` · `sync.pull(cursor)` · `call.offer / ring / answer / decline / end / ice-hint` · `ptt.request / grant / release / queue-pos` · `server.hint(drain|reconnect)`

**Internal:** NATS subjects — `msg.in`, `dev.{device}.out`, `push.dispatch`, `media.lifecycle`, `call.events`, `group.events`; gRPC only where synchronous cross-deployable calls are unavoidable (media-svc ↔ core-api quota check).

## Appendix C — Client Local Store (per device)

SQLCipher: `chats` · `messages` (+ FTS5 virtual table) · `attachments` (blob refs + local paths) · `outbox` (pending sends, retry state) · `contacts` · `signal_sessions` / `identity_keys` (hardware-backed keystore where available) · `settings` · `sync_cursors`.

## Appendix D — Epic Map (proposed 30-epic roadmap → this HLD) *(v1.1)*

Disposition of every epic from the proposed program breakdown. **Covered** = designed in v1.0; **Added v1.1** = scoped in by this revision; **Corrected** = the epic as written is impossible or wrong under this architecture and was redefined; **Deferred** = V3.

| # | Epic | Disposition | Where / phase |
|---|---|---|---|
| 0 | Project foundation | Covered | §17, §19, Appendix A — P0 |
| 1 | Identity & authentication | Covered | §5, §15.2 — P0 |
| 2 | User profile | Covered (+ user QR added v1.1) | §2.1, §7.1 — P0–P1 |
| 3 | Contacts | Covered (+ favorites, invite links added v1.1) | §2.1, §15.4 — P1 |
| 4 | 1:1 messaging | Covered; link previews **corrected** to sender-side (E2EE) | §8, §2.1 — P0 |
| 5 | Media | Covered; GIFs/stickers added v1.1; thumbnails/compression are client-side (correction #6) | §9 — P1 |
| 6 | Group chat | Covered | §8.3 — P1 |
| 7 | Channels / communities | **Deferred** — different product shape (§2.3) | V3 backlog |
| 8 | Presence | Covered | §8.5 — P0 |
| 9 | Notifications | Covered (+ mute/badge added v1.1) | §13 — P0 |
| 10 | Status / stories | Covered | §12 — P3 |
| 11 | Search | **Corrected**: content search is client-side FTS; no OpenSearch (correction #4) | §14 — P1+ |
| 12 | Voice calling | Covered | §10 — P2 |
| 13 | Video calling | Covered; recording **corrected** (client-side + consent, correction #9) | §10.4, §10.6 — P2 |
| 14 | Group calls | Covered — SFU = LiveKit (§10.1) | §10 — P2 |
| 15 | End-to-end encryption | Covered — libsignal; never roll your own | §15.1 — P0 |
| 16 | File storage | Covered — MinIO, presigned URLs, GC | §9, §7.5 — P1 |
| 17 | Admin dashboard | **Added v1.1** — deliberately narrowed by E2EE | §15.6 — P4 |
| 18 | Analytics | **Added v1.1** — metadata-only, self-hosted | §18.1 — P4 |
| 19 | Security | Covered | §15 — all phases |
| 20 | Performance optimization | Covered | §21 — all phases |
| 21 | Offline synchronization | Covered by the local-first design | §4.1, §7.3, §8.1 — P0 |
| 22 | Multi-device | Covered | §2.1, §15.1 — P3 |
| 23 | Monitoring & observability | Covered | §18 — P0 |
| 24 | DevOps | Covered | §17, §19 — P0 |
| 25 | Testing & QA | Covered | §19; load/pentest/chaos in P4 exit criteria |
| 26 | Mobile applications | Covered — React Native + Expo (Flutter not adopted, §1.1) | §6 — all phases |
| 27 | Web client | Covered — React + Vite PWA (Next.js rejected, correction #7); accessibility added v1.1 | §6, §2.1 — all phases |
| 28 | AI & smart features | **Deferred** — on-device / opt-in only under E2EE (§2.3) | V3 backlog |
| 29 | Production launch | Covered | §23 P4 |

---

*End of HLD. Next documents in sequence: ADR-001 (relay model), LLD per bounded context, protobuf contract definitions — to be produced per roadmap phase, before any code.*
