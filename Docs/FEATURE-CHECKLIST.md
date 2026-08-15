# WhatsApp V2 — Feature & Task Checklist

The complete feature set (65 functional requirements across 12 domains), with an
honest build status per layer. Source of truth: `Docs/01-requirements/
functional-requirements.md` (features) + `Docs/TASKS.txt` (T0–T4 build tasks).

## How to read the status

Each feature is tracked across three layers:

| Col | Layer | Meaning |
|---|---|---|
| **D** | Design | HLD + LLD + protocol contract decided |
| **S** | Server | Backend implemented + tested (Go services, Phase 0–4) |
| **C** | Client UI | Wired end-to-end in the mobile/web app so a user can actually use it |

- **✅** done · **🔶** partial / behind a code seam · **⬜** not started · **📋** V3 backlog

**The headline:** Design and Server are ✅ across the board (Phase 0–4 is code-complete).
**The remaining work to "build the whole app" is almost entirely the Client-UI column.**
The **web** client is a working messenger today: sign in, add-by-number, real
1:1 send/receive, live updates, ✓/✓✓/✓✓-blue receipts, typing + online/last-seen
presence, and message actions (reply/copy/edit/delete/star/pin) — all verified
live (see `Docs/BUILD-TASKS.md` Phase 5, T5.00–T5.05). Mobile parity + the rest
of the screens (media send, groups, calls, stories, settings) are the open work.

---

## AUTH — Identity & Authentication

| ID | Feature | D | S | C |
|---|---|:-:|:-:|:-:|
| FR-AUTH-01 | Register with phone + SMS/email OTP | ✅ | ✅ | ✅ |
| FR-AUTH-02 | OTP verify (6-digit, 10 min, 5 tries, rate-limited) | ✅ | ✅ | ✅ |
| FR-AUTH-03 | Optional 2FA registration PIN (Argon2id) | ✅ | ✅ | 🔶 |
| FR-AUTH-04 | Access JWT + rotating refresh, reuse detection | ✅ | ✅ | ✅ |
| FR-AUTH-05 | Device registry (1 primary + ≤4), QR link | ✅ | ✅ | 🔶 |
| FR-AUTH-06 | List / rename / revoke devices | ✅ | ✅ | ⬜ |
| FR-AUTH-07 | Logout (per device), session listing | ✅ | ✅ | 🔶 |
| FR-AUTH-08 | Account deletion (tombstone + ≤30d purge) + GDPR export | ✅ | ✅ | ⬜ |

## USER — Profile & Privacy

| ID | Feature | D | S | C |
|---|---|:-:|:-:|:-:|
| FR-USER-01 | Profile: username, display name, about (avatar image deferred) | ✅ | ✅ | ✅ |
| FR-USER-02 | Per-field privacy (last-seen/avatar/about/receipts) | ✅ | ✅ | ✅ |
| FR-USER-03 | Block / unblock | ✅ | ✅ | ✅ |
| FR-USER-04 | Per-user QR to add a contact | ✅ | ✅ | ⬜ |

## CONT — Contacts

| ID | Feature | D | S | C |
|---|---|:-:|:-:|:-:|
| FR-CONT-01 | Address-book sync via hashed numbers (no plaintext stored) | ✅ | ✅ | 🔶 |
| FR-CONT-02 | Search users by username | ✅ | ✅ | ✅ |
| FR-CONT-03 | Favorites; invite-a-friend link | ✅ | ✅ | ✅ |

## MSG — 1:1 Messaging

| ID | Feature | D | S | C |
|---|---|:-:|:-:|:-:|
| FR-MSG-01 | Send/receive E2EE text; sending→sent→delivered→read | ✅ | ✅ | ✅ |
| FR-MSG-02 | Delivery/read receipts (batched, privacy-gated) | ✅ | ✅ | ✅ |
| FR-MSG-03 | Typing indicator; online/last-seen | ✅ | ✅ | ✅ |
| FR-MSG-04 | Reply (quote), forward, copy | ✅ | ✅ | 🔶 |
| FR-MSG-05 | Delete-for-me / delete-for-everyone (48 h) | ✅ | ✅ | ✅ |
| FR-MSG-06 | Edit within 15 min | ✅ | ✅ | ✅ |
| FR-MSG-07 | Pin, star, emoji reactions | ✅ | ✅ | 🔶 |
| FR-MSG-08 | Sender-side link previews | ✅ | ✅ | 🔶 |
| FR-MSG-09 | Offline hold ≤30d, zero-loss delivery on reconnect | ✅ | ✅ | ✅ |
| FR-MSG-10 | Client-side full-text search (SQLite FTS5) | ✅ | ✅ | ✅ |

## GRP — Groups

| ID | Feature | D | S | C |
|---|---|:-:|:-:|:-:|
| FR-GRP-01 | Create group ≤1,024 (name, description; avatar image deferred) | ✅ | ✅ | ✅ |
| FR-GRP-02 | Roles owner/admin/member; promote/demote | ✅ | ✅ | ✅ |
| FR-GRP-03 | Add/remove members; leave; delete | ✅ | ✅ | ✅ |
| FR-GRP-04 | Invite links + QR join; revocation | ✅ | ✅ | 🔶 |
| FR-GRP-05 | Post permissions; announcements mode | ✅ | ✅ | ✅ |
| FR-GRP-06 | @mentions; pinned; aggregate receipts | ✅ | ✅ | 🔶 |
| FR-GRP-07 | Sender-Key encryption; rotation on membership change | ✅ | ✅ | 🔶 |

## MED — Media

| ID | Feature | D | S | C |
|---|---|:-:|:-:|:-:|
| FR-MED-01 | Images/video/audio/voice/docs ≤25 MB, multi-file | ✅ | ✅ | 🔶 |
| FR-MED-02 | Client compression + thumbnail/blurhash | ✅ | ✅ | 🔶 |
| FR-MED-03 | Client AES-256-GCM before upload (server sees ciphertext) | ✅ | ✅ | ✅ |
| FR-MED-04 | Resumable chunked uploads; progress; retry | ✅ | ✅ | ✅ |
| FR-MED-05 | GIFs & sticker packs (IP-hiding GIF proxy) | ✅ | ✅ | 🔶 |
| FR-MED-06 | In-chat playback/preview; download manager | ✅ | ✅ | ✅ |

## CALL — Voice & Video

| ID | Feature | D | S | C |
|---|---|:-:|:-:|:-:|
| FR-CALL-01 | 1:1 + group ≤32 voice/video, E2EE frames | ✅ | ✅ | 🔶 |
| FR-CALL-02 | Lock-screen ringing (CallKit/PushKit, FCM) | ✅ | ✅ | 🔶 |
| FR-CALL-03 | Ring state machine (45 s timeout) | ✅ | ✅ | ✅ |
| FR-CALL-04 | Mute/speaker/BT/camera-switch/screen-share/blur | ✅ | ✅ | 🔶 |
| FR-CALL-05 | Adaptive quality; video→audio downgrade; ICE restart | ✅ | ✅ | 🔶 |
| FR-CALL-06 | Call history (90 d); missed-call notifications | ✅ | ✅ | ✅ |
| FR-CALL-07 | Client-side recording w/ consent signaling | ✅ | ✅ | ⬜ |

## PTT — Push-to-Talk

| ID | Feature | D | S | C |
|---|---|:-:|:-:|:-:|
| FR-PTT-01 | Audio rooms ≤500, one speaker, press-and-hold | ✅ | ✅ | ⬜ |
| FR-PTT-02 | Server-authoritative floor (atomic acquire, fencing, FIFO) | ✅ | ✅ | ⬜ |
| FR-PTT-03 | Grant p95 ≤200 ms; heartbeat auto-release; 60 s cap | ✅ | ✅ | — |
| FR-PTT-04 | SFU publish-permission enforcement | ✅ | ✅ | 🔶 |

## STORY — Stories / Status

| ID | Feature | D | S | C |
|---|---|:-:|:-:|:-:|
| FR-STORY-01 | Photo/text stories, 24 h expiry (video same path) | ✅ | ✅ | ✅ |
| FR-STORY-02 | Audience at post time; per-story E2EE key | ✅ | ✅ | 🔶 |
| FR-STORY-03 | View receipts + viewer list (reactions deferred) | ✅ | ✅ | 🔶 |

## NOTIF — Notifications

| ID | Feature | D | S | C |
|---|---|:-:|:-:|:-:|
| FR-NOTIF-01 | Data-only push (zero plaintext through FCM/APNs) | ✅ | ✅ | 🔶 |
| FR-NOTIF-02 | VoIP push for calls; mention elevation | ✅ | ✅ | 🔶 |
| FR-NOTIF-03 | Per-chat/global mute; badge counts on-device | ✅ | ✅ | ⬜ |
| FR-NOTIF-04 | Offline UnifiedPush/ntfy driver | ✅ | ✅ | 🔶 |

## SYNC — Multi-Device & Offline

| ID | Feature | D | S | C |
|---|---|:-:|:-:|:-:|
| FR-SYNC-01 | Per-device Signal sessions | ✅ | ✅ | 🔶 |
| FR-SYNC-02 | History bootstrap to a new linked device | ✅ | ✅ | 🔶 |
| FR-SYNC-03 | Offline outbox + delta sync, UUIDv7 dedupe | ✅ | ✅ | ✅ |
| FR-SYNC-04 | Encrypted client backups (Argon2id) | ✅ | ✅ | 🔶 |

## ADMIN — Console, Trust & Safety

| ID | Feature | D | S | C |
|---|---|:-:|:-:|:-:|
| FR-ADMIN-01 | OIDC-SSO + 2FA admin, RBAC, IP allowlist (2FA/allowlist at edge) | ✅ | ✅ | ✅ |
| FR-ADMIN-02 | Metadata search; report queue; warn/suspend/ban | ✅ | ✅ | ✅ |
| FR-ADMIN-03 | Immutable audit_log on every action (SPA has a viewer) | ✅ | ✅ | ✅ |
| FR-ADMIN-04 | Feature-flag mgmt; config; aggregate dashboards (dashboards deferred) | ✅ | ✅ | 🔶 |
| FR-ADMIN-05 | Reports attach ciphertext only with consent | ✅ | ✅ | — |

> ADMIN "C" = a separate internal React SPA over the existing admin REST API — not built yet.

---

## Platform / Ops (not FR-numbered, but real deliverables)

- ✅ 5 Go deployables (core-api, ws-gateway, media-svc, notification-svc, rtc)
- ✅ Postgres 17 (partitioned inbox) · Valkey · NATS JetStream · MinIO · LiveKit+coturn
- ✅ Helm charts + ArgoCD, CI/CD, observability (Prometheus/Loki/Tempo/Grafana)
- ✅ Feature flags + kill switches · metadata-only analytics · crash reporting
- ✅ Self-host / fully-offline profile (ntfy, email-OTP, step-ca)
- ✅ `start.sh` one-box launcher · Android APK CI (in-app server URL)
- ⬜ Human launch gates: external pentest (T4.06), DR game-day (T4.07), 2-week SLO soak (GATE P4)

---

## V3 backlog (post-launch)

📋 Broadcast channels/communities · MLS mega-groups · on-device AI (transcribe/
smart-reply/translate) · PSI contact discovery · sealed sender · streaming PTT
tier · progressive video · CDN media · Tauri desktop · multi-region

---

## Build plan to finish V2 (the Client-UI column)

The server is done; these milestones wire the UI so every feature is usable.
Suggested order (each is a shippable increment):

- **M1 — Core chat polish** *(mostly done)*: 1:1 send/receive ✅, add read receipts, typing, presence into the thread UI; message actions (reply/forward/copy/delete/edit/react).
- **M2 — Media in chat**: attach picker → compress → encrypt → upload → bubble render + gallery + voice notes + download manager (shared logic exists; wire the send path + renderers).
- **M3 — Profile & privacy**: profile screen, avatar, privacy toggles, block/unblock, per-user QR.
- **M4 — Groups**: create/manage screen, members + roles, invite links/QR, mentions, announcements.
- **M5 — Calls UI**: call screen (1:1 then group grid), controls, incoming ring, history list.
- **M6 — Multi-device & settings**: device list/link/revoke, linked-device history bootstrap, backups UI, notification settings, account delete/export.
- **M7 — Stories & PTT**: story composer/viewer/receipts; PTT room + press-and-hold floor UI.
- **M8 — Admin SPA**: internal React app over the admin REST API (reports, users, flags, dashboards).

Legend for updating this file: flip **C** cells ⬜→🔶→✅ as screens land; keep D/S as the contract.
