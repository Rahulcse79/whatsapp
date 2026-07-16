# Task Breakdown — Implementation Order

| Doc | Dependency-ordered tasks (first actionable cut; expands per sprint) |
|---|---|
| Status | v1.0 · Convention: T-`<phase>`.`<nn>` · Each task = one AI-assistable unit: read linked docs → implement → tests green → merge. Supersedes-compatible with WhatsApp-V2-Tasks.xlsx (15 macro-tasks → these are their decomposition). |

## P0 — Foundations (order is the dependency graph)

| # | Task | Docs to read first |
|---|---|---|
| T0.01 | Monorepo scaffold: Go workspace, clients pnpm workspace, buf setup, Makefile | coding-standards, git-workflow |
| T0.02 | Proto contracts v1: WS frames, events, gRPC (compile-only) | websocket-protocol, internal-events-nats |
| T0.03 | Compose dev stack: PG+Valkey+NATS+MinIO+ntfy+grafana bundle | kubernetes-deployment §3, offline-local-server §5 |
| T0.04 | CI pipeline: PR gates + main build + image signing | ci-cd |
| T0.05 | platform/ package: config, pg pool, valkey, nats, otel, UUIDv7, GCRA limiter | core-api-lld §1, DS&A §7 |
| T0.06 | Import-boundary lint + PR template + docs-contract check | coding-standards |
| T0.07 | DB migrations 001: users/devices/prekeys/conversations/message_inbox (+partitions) | database-design |
| T0.08 | auth context: OTP flow (drivers: mock, SMS, email), JWT/refresh, PIN | auth-users-api, security-architecture §2 |
| T0.09 | devices + keys contexts: registry, revocation, prekey publish/fetch | e2ee-design §2/§5 |
| T0.10 | ws-gateway skeleton: upgrade, Hello/JWT, registry shards, ping/pong | ws-gateway-lld |
| T0.11 | chat accept pipeline: dedupe, seq, inbox write, MsgAck | core-api-lld §2, DS&A §1–3 |
| T0.12 | NATS delivery: dev.*.out publish, gateway consumer, write-pump backpressure | internal-events-nats, ws-gateway-lld §2 |
| T0.13 | resume/replay: cursors, InboxBatch, ClientAck delete, interleave rule | websocket-protocol §3, ws-gateway-lld §3 |
| T0.14 | receipts (cumulative + coalescing) + delivery-state machine | DS&A §9 |
| T0.15 | presence + typing (subscription model, privacy gate) | DS&A §10–11 |
| T0.16 | notification-svc: dispatch consumer, FCM/APNs/ntfy drivers, breaker | notification-svc-lld |
| T0.17 | clients/packages: proto-types, crypto-wrapper (libsignal), sync-engine core (outbox/cursors) | offline-sync-local-store |
| T0.18 | RN app shell: auth screens, chat list/thread, local DB, WS client | mobile-app-architecture |
| T0.19 | Web PWA shell: same features, workers, WebPush | web-app-architecture |
| T0.20 | E2EE integration: X3DH+ratchet in send/receive path (both clients) | e2ee-design §2 |
| T0.21 | overlays: edit/delete windows, reactions, pin/star | messaging semantics, sequence-diagrams §9 |
| T0.22 | K8s deploy: helm charts, ArgoCD apps, canary rollout, drain | kubernetes-deployment |
| T0.23 | Observability: dashboards 1–3, SLO alerts, synthetic probe | monitoring + slos docs |
| T0.24 | Protocol-test framework + scenarios P1–P5, P7–P10 | test-strategy §3 |
| **Gate** | **P0 exit: chaos-kill gateway, zero loss (P3 scenario) in staging** | roadmap-milestones |

## P1 — Groups & media

| # | Task |
|---|---|
| T1.01 | groups context: CRUD, roles, permissions, invite links/QR, announcements |
| T1.02 | group events → Sender-Key rotation protocol (clients) + membership cache |
| T1.03 | group fan-out worker (batched, async) + aggregate receipts |
| T1.04 | media-svc: upload sessions, presign, verify, quotas |
| T1.05 | client media pipeline: compress/encrypt/thumb/blurhash + resumable upload |
| T1.06 | media UX: gallery, voice notes, documents, download manager |
| T1.07 | GIF proxy + sticker packs |
| T1.08 | GC job + lifecycle rules |
| T1.09 | contacts: hashed sync + defenses, search, favorites, invites |
| T1.10 | client FTS5 search + search UX |
| T1.11 | sender-side link previews |
| T1.12 | protocol tests P6, P13; fan-out load profile |
| **Gate** | 1,024-member group + 25 MB resumable upload under load |

## P2 — Calling

T2.01 rtc infra (LiveKit pool, coturn, host-network node pool) · T2.02 call-ctl: rooms, tokens, ring machine, webhooks · T2.03 1:1 voice (both clients) + E2EE frames · T2.04 lock-screen ring: CallKit / ConnectionService + VoIP push path · T2.05 video + simulcast + camera mgmt · T2.06 screen share + blur (on-device) · T2.07 group calls ≤ 32 + active speaker + layouts · T2.08 call history + missed-call flow · T2.09 quality adaptation + ICE-restart recovery · T2.10 protocol test P12 + call surge load. **Gate:** setup p95 ≤ 3 s; locked-phone ring both platforms.

## P3 — Multi-device, PTT, stories

T3.01 device linking QR + signed device lists · T3.02 history bootstrap transfer · T3.03 per-device session fan-out + self-sync · T3.04 PTT: floor Lua, WS frames, SFU perm flip, room UX · T3.05 stories: keys, audience snapshot, expiry, viewers · T3.06 encrypted backups (create/restore) · T3.07 protocol tests P11 + multi-device suite · **Gate:** PTT p95 ≤ 200 ms @ 200 listeners; link/revoke clean.

## P4 — Hardening & launch

T4.01 admin console (SSO, RBAC, report queue, actions, audit) · T4.02 feature-flag mgmt UI · T4.03 analytics rollups + product dashboard + GlitchTip · T4.04 full load suite (all profiles) + durability audit · T4.05 chaos suite always-on staging · T4.06 external pentest + remediation · T4.07 DR game day (RTO/RPO proof) · T4.08 runbooks + on-call setup · T4.09 UAT (FR×AC matrix, accessibility audit) · T4.10 offline-profile validation (full suite on single box) · T4.11 go-live checklist + staged launch · **Gate:** SLOs green 2 weeks, chaos on.

## Working agreement

One task = one PR-sized unit where possible (T0.11-class tasks split further at sprint planning). Every task PR links its task ID + docs; done = merged + tests + docs current. **Never ask an AI to implement beyond one task's scope in a single pass.**
