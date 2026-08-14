# Build Tasks — full feature runner (V2 completion → V3/V4)

Run these **one at a time**, in order. Each task is a self-contained, shippable
increment: copy its `PROMPT>` line to start it. Mirrors the style of
`Docs/TASKS.txt` (which built the V2 backend, T0–T4).

- `[ ]` open · `[~]` in progress · `[x]` done
- Finishing a task: flip its box, commit + push, then run the next.
- Status of the underlying features: `Docs/FEATURE-CHECKLIST.md` (D/S/C columns).

---

## Architect's reconciliation (read once)

The V2 platform is **already built and production-grade** at the architecture +
backend layer (5 Go services, Postgres/Valkey/NATS/MinIO/LiveKit, E2EE, Helm/
ArgoCD/CI, observability). **These tasks are mostly the client-UI frontier plus
new features** — do NOT rebuild the backend. Where this spec names tech that
differs from V2's decided stack, the pragmatic call:

- **E2EE is non-negotiable and constrains "AI/search over messages."** The
  server cannot read content. So: AI (translation, summaries, smart replies,
  transcription, moderation) runs **on-device or on explicitly user-disclosed
  content**; full-text message search stays **client-side (SQLite FTS5)**.
  Server-side Elasticsearch is only for **public/metadata** surfaces (channels,
  user/username discovery, public communities).
- **Kafka/RabbitMQ → NATS JetStream** already fills the event-bus/queue role at
  20k concurrent. Add Kafka only if a task's scale profile proves NATS is the
  bottleneck (a dedicated task below evaluates this — don't add it speculatively).
- **Cassandra → partitioned PostgreSQL** already serves the inbox at target
  scale. Add Cassandra only behind the same evidence gate (task P.03).
- **New realtime fan-out (channels/communities/webinars)** rides the existing
  WS gateway + NATS, not a parallel stack.

Everything else in the spec is additive UI + new bounded contexts.

---

# PHASE 5 — Finish the V2 client experience (make what's built usable)

The backend for all of this exists; these wire the mobile + web UI end-to-end.

- [ ] **T5.00  Fix live message delivery — THE BLOCKER, do this first**
  PROMPT> the app's WsClient never establishes the WebSocket (0 ESTABLISHED on :8081, core-api logs no chat.Accept, sends stay "sending") even with a fresh valid token and a reachable, origin-permitting endpoint — a raw `new WebSocket('ws://<ip>:8081/v1/ws')` from the same page OPENS (101), so transport+origin are fine. Debug the client wiring: AppServices.startRealtime → createWsTransportFactory(config.wsUrl) → WsClient.start (client-core wsClient.ts). Check start() is actually called, the Hello frame + token are sent/accepted, and the transport builds the right URL. Fix so two logged-in clients exchange live messages. Nothing in Phase 5+ delivers until this works. (Shared client-core → unblocks BOTH web + mobile.)
- [~] **T5.01  Real 1:1 messaging** *(web mostly done this session; awaits T5.00; mobile next)*
  PROMPT> WEB done this session (commits 3cdef47+6b16f0b): add-by-number "new chat" → POST /v1/conversations/direct, conversation-shared AES-GCM dev cipher, decrypt-on-receive — VERIFIED LIVE that A opens the real shared conversation with B by number; live message exchange is gated on T5.00. NEXT: mobile parity (convCrypto.ts scaffolded, uncommitted), then verify two devices exchange readable messages.
- [ ] **T5.02  Live thread updates + chat list refresh**
  PROMPT> push a store-change signal from persistInboxBatch/markSent so the open thread + chat list update in real time without leaving the screen. Both clients.
- [ ] **T5.03  Message status + read receipts UI**
  PROMPT> render sent/delivered/read as ticks; send READ receipts when a thread is viewed (privacy-gated per FR-USER-02); show delivery state on my bubbles. Both clients.
- [ ] **T5.04  Typing + presence UI**
  PROMPT> send throttled typing (1/3s) on compose; show "typing…" and online/last-seen in the thread header per privacy settings. Both clients.
- [ ] **T5.05  Message actions (long-press menu)**
  PROMPT> reply (quote), forward, copy, delete-for-me, delete-for-everyone (48h), edit (15m), star, pin, react (single + multiple) — wired to the existing overlay events. Both clients.
- [ ] **T5.06  Media send in chat**
  PROMPT> composer attach button → gallery/camera/document picker → media-pipeline compress→encrypt→resumable upload → media bubble (render already exists). Voice-note record + playback. Progress + retry. Both clients.
- [ ] **T5.07  Profile & privacy screens**
  PROMPT> profile (username, display name, avatar, about), per-field privacy toggles (last-seen/avatar/about/receipts: everyone|contacts|nobody), block/unblock list, per-user QR. Both clients.
- [ ] **T5.08  Contacts screen + sync**
  PROMPT> address-book sync (hashed handles → /v1/contacts/sync), matched-contacts list, search-by-username, favorites, invite link. Both clients.
- [ ] **T5.09  Groups UI**
  PROMPT> create/edit group, member list + roles (owner/admin/member, promote/demote), add/remove/leave, invite links + QR join, announcements mode, @mentions, pinned. Both clients.
- [ ] **T5.10  Calls UI**
  PROMPT> outgoing/incoming 1:1 call screen (LiveKit join), controls (mute/speaker/camera-switch/screen-share/blur), incoming ring surface, then group grid ≤32, then call-history list. Both clients.
- [ ] **T5.11  Stories/Status UI**
  PROMPT> status composer (image/video/text), story ring on chat list, viewer with tap-through, audience selector, view receipts + viewer list, reactions. Both clients.
- [ ] **T5.12  Multi-device linking + settings**
  PROMPT> device list/rename/revoke, QR link a new device (crypto-wrapper deviceList), linked-device history bootstrap (sync-engine), encrypted backup create/restore UI, notification settings, account delete/export. Both clients.
- [ ] **T5.13  Notifications UI + push registration**
  PROMPT> data-only push receive → decrypt → render, per-chat + global mute, badge counts on-device, VoIP push for calls, mention elevation, notification history. Both clients.
- [ ] **T5.14  Admin SPA**
  PROMPT> internal React app over the admin REST API: OIDC login, report queue, user metadata search + actions, feature-flag console, audit-log viewer, aggregate dashboards.
- [ ] **T5.15  Chat conveniences**
  PROMPT> favorite/archive/mute chats, chat wallpaper + per-chat theme, dark/light system theming, chat export, jump-to-original-message, undo-send window, drafts persistence.

# PHASE 6 — Rich & advanced messaging (new)

- [ ] **T6.01  Rich composer**
  PROMPT> markdown + rich-text formatting, emoji picker, GIF search (existing IP-hiding proxy), sticker + custom-sticker picker. Both clients.
- [ ] **T6.02  Polls**
  PROMPT> new bounded context internal/polls (create/vote/close, E2EE payload); poll message type + composer + results UI. Server + clients + tests.
- [ ] **T6.03  Live location + contact sharing**
  PROMPT> live-location share (time-boxed, E2EE position updates over WS) + static location + contact-card message types. Server relay + clients.
- [ ] **T6.04  Scheduled messages + drafts + templates**
  PROMPT> schedule a message (client-held outbox + send-at, or server scheduler for offline), saved replies/templates, auto-reply rules. Clients (+ minimal server for offline scheduling).
- [ ] **T6.05  Advanced search UX**
  PROMPT> search by user / by date / by file / by hashtag over the local FTS5 index; jump-to-result; per-chat search. Both clients.

# PHASE 7 — Channels (V3)

- [ ] **T7.01  Channels backend**
  PROMPT> new bounded context internal/channels: public/private/verified channels, followers, broadcast fan-out (ride WS gateway + NATS), scheduled posts, reactions, comments, admin roles. Server + migration + tests.
- [ ] **T7.02  Channels UI**
  PROMPT> discover/browse, follow, channel feed, broadcast composer, reactions/comments, channel insights. Both clients.
- [ ] **T7.03  Channel analytics + monetization**
  PROMPT> per-channel metadata analytics (views/followers/reach, privacy-preserving), premium subscription hooks (payments seam). Server + admin dashboards.

# PHASE 8 — Communities (V3)

- [ ] **T8.01  Communities backend**
  PROMPT> internal/communities: a community groups many groups + an announcement group, community roles, shared events/calendar, moderation, discovery. Server + migration + tests.
- [ ] **T8.02  Communities UI**
  PROMPT> community home, grouped groups, announcements, shared calendar/events, roles + moderation, discover + invite management. Both clients.

# PHASE 9 — Advanced calls (V3/V4)

- [ ] **T9.01  Call quality + effects**
  PROMPT> noise suppression, echo cancellation, virtual background/replacement, PiP, spatial audio hooks — client-side over the existing call-engine.
- [ ] **T9.02  Webinar / live mode**
  PROMPT> webinar (1-to-many), waiting room, raise-hand, in-call polls + Q&A, attendance reports, live captions (on-device STT), real-time translation (on-device). Server call-ctl extensions + clients.
- [ ] **T9.03  Breakout rooms + streaming + recording**
  PROMPT> breakout rooms, live streaming out (RTMP/HLS egress via LiveKit), client-side recording with consent signaling, multi-camera, 4K profile. rtc + call-ctl + clients.

# PHASE 10 — Security & privacy (expand)

- [ ] **T10.01  Secret chats + self-destruct**
  PROMPT> per-chat disappearing timers, view-once media, self-destruct messages, hidden/locked chats, screenshot-protection signaling. Clients + minimal server TTL support.
- [ ] **T10.02  Device auth hardening**
  PROMPT> biometric/Face-ID/fingerprint unlock, passkeys (WebAuthn) as a 2FA/login path, secure key storage audit, suspicious-login + IP monitoring surfaces. Clients + auth service.
- [ ] **T10.03  Anti-abuse**
  PROMPT> anti-spam/phishing/scam heuristics (metadata + on-device link checks), rate-limit tuning, report→admin pipeline UX, block/report flows everywhere. Server + clients.

# PHASE 11 — AI features (E2EE-safe: on-device / opt-in)

- [ ] **T11.01  On-device AI runtime**
  PROMPT> client AI runtime abstraction (on-device model or user-opt-in server endpoint), consent + disclosure UX, kill-switch flag. No server access to E2EE content.
- [ ] **T11.02  Messaging AI**
  PROMPT> smart replies, grammar correction, translation, summarization, voice→text transcription, text→speech — all on-device or on explicitly disclosed content. Clients.
- [ ] **T11.03  AI moderation + assistant**
  PROMPT> on-device toxicity/spam detection, AI chatbot/assistant, semantic search over local history, conversation/meeting summary, auto-tagging, intent detection. Clients (+ optional opt-in server).

# PHASE 12 — Collaboration (V4)

- [ ] **T12.01  Shared notes + tasks**
  PROMPT> internal/collab: shared notes, shared task lists, comments, approvals, version history, presence + activity timeline. Server (CRDT/OT) + clients.
- [ ] **T12.02  Whiteboard + live collaboration**
  PROMPT> real-time whiteboard + file collaboration over WS (CRDT), team workspace, shared calendar. Server + clients.

# PHASE 13 — Discovery & platform (metadata-only)

- [ ] **T13.01  Elasticsearch for public/metadata search**
  PROMPT> internal/discovery: OpenSearch/Elasticsearch index for channels, public communities, usernames — metadata only, never E2EE content. Server + sync pipeline + tests.
- [ ] **T13.02  Bots, mini-apps, interactive messages**
  PROMPT> bot framework (webhook API), interactive messages (buttons/forms/rich cards), mini-app container, saved templates. Server + clients.

# PHASE 14 — Notifications & multi-channel (expand)

- [ ] **T14.01  Multi-channel notifications**
  PROMPT> email + SMS notification drivers (offline: ntfy already wired), notification channels, snooze, scheduled notifications, sound/vibration settings, desktop notifications. notification-svc + clients.

# PHASE 15 — Scale & ops hardening (evidence-gated)

- [ ] **T15.01  Load + capacity re-baseline**
  PROMPT> run the k6 load suite (fanout/callsurge/ptt + new channel/webinar profiles) at target scale; capture where NATS/Postgres are the bottleneck. Report only.
- [ ] **T15.02  Kafka evaluation (only if T15.01 proves need)**
  PROMPT> IF load shows NATS is the bottleneck for a specific stream, introduce Kafka for that stream behind the existing port; otherwise document why NATS stays. Evidence-gated.
- [ ] **T15.03  Cassandra evaluation (only if T15.01 proves need)**
  PROMPT> IF the partitioned Postgres inbox is the bottleneck, prototype a Cassandra inbox behind the InboxWriter port; otherwise document why Postgres stays. Evidence-gated.
- [ ] **T15.04  CDN + object-storage delivery**
  PROMPT> front MinIO with a CDN for media/thumbnails (signed URLs), progressive/streaming video, background upload/download tuning. media-svc + infra.
- [ ] **T15.05  Payments/monetization backbone**
  PROMPT> internal/payments: premium subscriptions, channel monetization, P2P transfer seam (PSP integration behind a port, never handling raw card data). Server + admin.

# GATES

- **After Phase 5:** the app is a fully usable WhatsApp-class messenger (all V2 features tappable).
- **After Phase 8:** feature-parity with Telegram/Discord community shape.
- **After Phase 12:** Slack/Teams-class collaboration.
- Human gates from V2 still apply before public launch: external pentest, DR drill, 2-week SLO soak.

---

## How to run

Pick the lowest open `[ ]` task and paste its `PROMPT>` (e.g. *"implement task
T5.02 …"*). I build it on the existing V2 code, commit + push, CI verifies, and
we flip the box. Phase 5 first — it turns the finished backend into a usable app.
