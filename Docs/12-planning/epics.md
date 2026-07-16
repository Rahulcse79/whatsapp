# Epic Catalog

| Doc | All epics: goal, features, key stories, phase, doc links |
|---|---|
| Status | v1.0 · Numbering matches HLD Appendix D (the 30-epic roadmap, corrected). Stories referenced as US-* expand per [user-stories-personas.md](../01-requirements/user-stories-personas.md) §2 before entering a sprint. |

## E0 — Project Foundation *(P0)*
**Goal:** engineering base that makes every later epic cheap.
Monorepo + import-boundary lint · buf proto pipeline · Compose dev stack · CI gates ([ci-cd.md](../08-devops/ci-cd.md) §2) · K8s+ArgoCD+Rollouts · LGTM observability · secrets (SOPS) · coding standards + PR template.
**Done when:** `docker compose up` boots the full infra; a hello-world frame round-trips client→gateway→core→NATS→client in dev with a trace visible in Grafana.

## E1 — Identity & Authentication *(P0)* — FR-AUTH
OTP request/verify (SMS+email drivers) · 2FA PIN · JWT+refresh rotation · device registry · revocation · sessions · account delete/export. Stories: US-AUTH-01…08. Docs: [auth-users-api.md](../04-api/auth-users-api.md), security-architecture §2.

## E2 — User Profile *(P0–P1)* — FR-USER
Profile CRUD · avatar (needs E5 minimal) · privacy settings enforcement (server-side at presence/receipt paths) · user QR · block/unblock.

## E3 — Contacts *(P1)* — FR-CONT
Hashed sync + enumeration defenses (threat-model T11) · username search · favorites · invite links.

## E4 — 1:1 Messaging *(P0)* — FR-MSG ← **the critical path**
WS protocol frames · accept pipeline (dedupe/seq/inbox) · delivery+receipts · typing/presence · resume/delta sync · overlays: reply, forward, edit, delete, reactions, pin, star · sender-side link previews · client FTS.
Stories: US-MSG-01 (exemplar) …-14. Docs: [websocket-protocol.md](../04-api/websocket-protocol.md), core-api-lld §2, DS&A §1–3, §6, §9.

## E5 — Media *(P1)* — FR-MED
Client pipeline (compress/encrypt/thumb/blurhash) · resumable multipart upload · download mgmt · voice notes · documents · GIF proxy + stickers · GC. Docs: [media-stories-api.md](../04-api/media-stories-api.md), media-svc-lld.

## E6 — Group Chat *(P1)* — FR-GRP
CRUD/roles/permissions · invite links+QR · announcements · mentions · Sender-Key fan-out + rotation on membership change · aggregate receipts. Docs: messaging-groups-api, e2ee-design §3.

## E7 — Channels/Communities *(V3 — deferred, HLD §2.3)*
One-to-many follower-graph product; own bounded context; **not** E2EE-pairwise. No V2 work.

## E8 — Presence *(P0)* — subscription model, privacy-gated, flap damping (DS&A §10).

## E9 — Notifications *(P0)* — FR-NOTIF
Data-only pushes · provider drivers (FCM/APNs/ntfy/WebPush) · VoIP path · mute/badge · dispatch pipeline (notification-svc-lld).

## E10 — Stories *(P3)* — FR-STORY
Per-story keys · audience snapshots · 24 h expiry jobs · viewers/reactions.

## E11 — Search *(P1, client-side)* — FTS5 index + search UX; server metadata search (trigram). ADR-005 binding.

## E12 — Voice Calling *(P2)* — FR-CALL
call-ctl ring machine · LiveKit+coturn deploy · 1:1 voice · lock-screen ring integrations · history · quality adaptation.

## E13 — Video Calling *(P2)* — camera/switch · simulcast ladder · screen share · on-device blur · PiP.

## E14 — Group Calls *(P2)* — ≤ 32; active-speaker; layouts; E2EE key epochs on join/leave.

## E15 — E2EE Core *(P0, cross-cutting)* — libsignal integration: sessions, prekey lifecycle, device-list signing, safety numbers, key-change UX. Doc: e2ee-design (binding). **Gate for E4 — no plaintext-phase messaging ever ships.**

## E16 — File Storage Platform *(P0 infra / P1 features)* — MinIO EC pool · presign service · quotas · lifecycle/GC · encrypted backups (FR-SYNC-04).

## E17 — Admin Console & T&S *(P4)* — FR-ADMIN — SSO+RBAC SPA · report queue · actions · audit log · flag management. HLD §15.6 (narrowed by E2EE).

## E18 — Analytics *(P4)* — metadata-only rollups + GlitchTip crash reporting (HLD §18.1). No content signals — enforcement list e2ee-design §8.

## E19 — Security Hardening *(cross-cutting; audit P4)* — threat-model upkeep · rate-limit implementation · attestation (online profile) · pentest remediation.

## E20 — Performance *(cross-cutting)* — budgets + bottleneck register ownership (performance-optimization.md); load-test findings.

## E21 — Offline Sync *(P0 core)* — outbox/cursors/conflict rules (offline-sync-local-store.md — binding for both clients).

## E22 — Multi-Device *(P3)* — linking QR flow · signed device lists · history bootstrap · per-device sessions · revocation UX.

## E23 — Observability *(P0)* — OTel wiring · dashboards as code · SLO alerts · synthetic probes.

## E24 — DevOps *(P0, then continuous)* — clusters · GitOps · canary · migrations discipline · DR drills · **offline profile** (E24b: ntfy/step-ca/Gitea/Harbor overlay — HLD §17.5).

## E25 — Testing Infrastructure *(P0, then continuous)* — Testcontainers harness · protocol-test framework (the P1–P15 catalog) · device farm · load rigs · durability auditor.

## E26 — Mobile App *(P0–P4)* — RN shell · platform adapters (push/CallKit/keystore/SQLCipher) · per-epic feature UIs · OTA/release lanes.

## E27 — Web App *(P0–P4)* — PWA shell · workers (DB/crypto) · WebPush · per-epic UIs · accessibility gate.

## E28 — AI Features *(V3 — deferred)* — on-device only under E2EE ([ai-features-v3.md](../14-ai/ai-features-v3.md)).

## E29 — Production Launch *(P4)* — UAT · security audit · load sign-off · DR validation · runbooks · go-live checklist · post-launch support rota.

---

### Dependency spine (what blocks what)

```
E0 → E15 → E4 → { E6, E5, E11, E21 } → E10
E0 → E1 → E2/E3            E4+E9 → push-complete messaging
E0 → E16 → E5              E12 → E13 → E14 → (PTT in E12 infra, P3)
E23/E24/E25 run with P0 and never stop      E17/E18 → P4       E26/E27 track every server epic
```
Estimated volume: ~180 features, ~700–900 tasks — first cut in [task-breakdown.md](task-breakdown.md).
