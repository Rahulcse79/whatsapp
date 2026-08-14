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

- [x] **T5.00  Fix live message delivery — THE BLOCKER** *(done — real 1:1 chat works)*
  ROOT CAUSE (two bugs): (1) wire-codec mismatch — the web transport sent frames as JSON but ws-gateway decodes binary protobuf, so the handshake was rejected with 4400 "expected hello frame" (messages never left the client). (2) `newId()` produced a fake `ts-rand` id, but `chat.Accept` requires a real UUIDv7 (`internal/platform/id.Parse`), so sends were rejected. FIX: wired `@wa/proto-types` (generated protobuf-es) + `@bufbuild/protobuf` into web and rewrote `wsTransport.ts` to a binary protobuf codec (client-core frames ↔ wsv1.Frame); made `newId()` emit spec UUIDv7. VERIFIED LIVE in two browser sessions (localhost + 127.0.0.1 origins): A "add-by-number" → real shared conversation → **A→B and B→A messages delivered and decrypted end-to-end**. NEXT: bring the same protobuf transport to mobile (rn wsTransport), then T5.01+.
- [~] **T5.01  Real 1:1 messaging** *(web mostly done this session; awaits T5.00; mobile next)*
  PROMPT> WEB done this session (commits 3cdef47+6b16f0b): add-by-number "new chat" → POST /v1/conversations/direct, conversation-shared AES-GCM dev cipher, decrypt-on-receive — VERIFIED LIVE that A opens the real shared conversation with B by number; live message exchange is gated on T5.00. NEXT: mobile parity (convCrypto.ts scaffolded, uncommitted), then verify two devices exchange readable messages.
- [x] **T5.02  Live thread updates + chat list refresh** *(web done; mobile with T5.01)*
  DONE (web): AppServices now has an `onChange` emitter fired on inbound batches, send-acks, and sends; the Thread + ChatList screens subscribe to it and re-fetch, replacing the old 1s/1.5s busy-poll with instant event-driven refresh (a 5s safety poll remains). VERIFIED LIVE: B sitting in the open thread saw A's second message ("LIVE UPDATE msg2 ⚡") appear instantly, no send/navigation. Mobile parity rides T5.01.
- [x] **T5.03  Message status + read receipts UI** *(web done; mobile with T5.01)*
  DONE (web): full ✓ / ✓✓ / ✓✓-blue ticks. client-core gained a server→client `Receipt` frame + `WsClient.onReceipt` + `MemoryMessageRepo.markReceipt` (monotonic state rank sending<sent<delivered<read). Web sends a DELIVERED receipt when it persists an inbox batch, and a READ receipt when the thread is on screen (`AppServices.markRead`); incoming receipts upgrade my bubbles' state; `StatusTicks` renders them. VERIFIED LIVE: A→B "tick test" showed ✓ then ✓✓ (B received, chat-list only) then ✓✓ **blue** once B opened the thread. Privacy-gating of receipts (FR-USER-02: last-seen/receipts everyone|contacts|nobody) is a later refinement — currently always sent in dev. Mobile parity rides T5.01.
- [x] **T5.04  Typing + presence UI** *(web done + verified live; mobile with T5.01)*
  DONE (web): client-core gained bidirectional `Typing` + `PresenceSub`/`PresenceUpdate` frames + `WsClient.onTyping`/`onPresence`; the web protobuf transport encodes/decodes them; AppServices tracks `typingByConv`/`presenceByUser`, sends throttled typing (1/3s) on compose + a stop-typing on send, and subscribes to the peer's presence on thread open (peer resolved from `startDirectChat` for the initiator and from the inbound message's `senderUserId` for the recipient). The Thread header renders a `statusLine`: **typing… › online › last seen …**. Server side was already wired (ws-gateway presenceConnect/Disconnect publish online/last-seen with flap-debounce; handleTyping → PublishTyping; handlePresenceSub → subscribe+snapshot). VERIFIED LIVE (two sessions, localhost + 127.0.0.1): A↔B both show **online**; A types → B header shows **typing…**, and B types → A shows **typing…**; alongside the ✓✓ receipts. Privacy-gating of presence (FR-USER-02) stays AllowAll in dev — a later refinement. Mobile parity rides T5.01.
- [~] **T5.05  Message actions (menu)** *(web done + verified live; forward + reactions deferred to T5.05b; mobile with T5.01)*
  DONE (web): each bubble has a **⋯ action menu** — reply, copy, star, pin, edit (mine, <15 m), delete-for-everyone (mine, <48 h), delete-for-me. Built the missing overlay-SEND infra: `OutgoingDraft`/`MsgSend` now carry `kind`+`overlayTarget`, `MemoryMessageRepo.enqueueOutgoing` applies edit/delete optimistically to the target (no new bubble) and `pendingSends` emits the right overlay kind; inbound `OVERLAY_EDIT` rewrites the body (via a new `OverlayApply.msgUuid` that keys the decrypted edit text); `deleteForMe` hides locally. Reply carries a `QuotedRef` in the sealed body (media-pipeline `encode/parseTextMessage`), rendered as a quote box. Worker RPC + web AppServices methods (editMessage/deleteForEveryone/deleteForMe/togglePin/toggleStar); Thread UI has an edit/reply composer bar + edited/star/pin indicators. VERIFIED LIVE (localhost session): sent a message → **edited it** (bubble shows new text + "(edited)"), **starred it** (⭐ + menu flips to Unstar), **replied** (quote box renders the original above the reply), **deleted-for-everyone** ("This message was deleted"). The peer-side overlay apply is unit-tested (client-core 54 + media-pipeline 67 green) and rides the same wire path proven 2-session in T5.00–T5.04. **DEFERRED to T5.05b:** forward (needs a conversation picker) + reactions (needs a reaction storage model + REACTION-overlay render). Mobile parity rides T5.01.
- [ ] **T5.05b  Forward + reactions**
  PROMPT> forward a message to another conversation (picker) + emoji reactions (single + multiple) using the REACTION overlay kind — store + render on both bubbles. Web then mobile.
- [x] **T5.06  Media send in chat** *(web done + verified live; voice-note record + camera + compression/thumbnail deferred; mobile with T5.01)*
  DONE (web): composer **📎 attach** → file picker → `MediaPipeline.prepare` (encrypt via the file key + **resumable multipart upload** to media-svc) → `encodeMediaMessage` sealed in the conversation body → media bubble (render already existed). New `web/src/platform/mediaUpload.ts` (UploadTransport: create/presign/complete + direct-to-object-storage part PUT, **refreshes the token on 401**); `config.mediaBaseUrl` (media-svc is a separate :8082 deployable); download transport repointed to it too; `AppServices.sendMedia`. Unblocked 3 server-integration gaps found live: (1) **CORS on media-svc** — added the same dev-CORS core-api uses; (2) **QuotaService gRPC unbuilt** in core-api → wired a dev/offline **NoopQuota** (prod keeps the real client); (3) upload transport now refreshes the 10-min access token on 401. VERIFIED LIVE: injected a PNG → create→presign→**PUT to MinIO**→complete → the ciphertext object landed in the `media` bucket and a **media bubble rendered** in the thread. server build+vet+media tests green; web typecheck green. **DEFERRED:** voice-note record/playback, camera capture, client compression + thumbnail/blurhash (the pipeline ports are optional — upload is raw bytes today). Mobile parity rides T5.01.
- [x] **T5.07  Profile & privacy screens** *(web done + verified live; avatar image upload + per-user QR deferred; mobile with T5.01)*
  PROMPT> profile (username, display name, avatar, about), per-field privacy toggles (last-seen/avatar/about/receipts: everyone|contacts|nobody), block/unblock list, per-user QR. Both clients.
  DONE (web): new **`internal/profile`** bounded context (content-free — identity metadata only) over the existing `users` (username/display_name/about/privacy jsonb, migration 000002) + `blocks` (000004) tables. Service does the light validation clients can't be trusted with (display ≤100, about ≤200, username 3–30 `[a-z0-9_.]` case-folded, privacy value ∈ everyone|contacts|nobody, no self-block). REST (all bearer-gated): `GET/PUT /v1/me`, `PUT /v1/me/privacy`, `GET /v1/users/{id}` (public view, UUID-validated), `GET/POST/DELETE /v1/blocks`. Wired in `cmd/core-api/main.go`. Client: `AppServices` gained `getMyProfile`/`updateMyProfile` (409→"username taken"), `saveMyPrivacy`, `loadUserProfile`+`peerNameOf`+`nameForUser` (userId→display-name cache that re-renders open screens on arrival), `blockUser`/`unblockUser`/`getBlocked`, and a `fetch`-based `authedRequest` (GET/PUT/DELETE + refresh-on-401). New **Profile screen** (`👤 You` in the chat-list head): edit display-name/username/about, 4 privacy `<select>`s (optimistic save), and a blocked list with Unblock. Thread header + chat-list rows now show the **peer's display name** instead of the raw conversation id. VERIFIED LIVE: signed in, saved profile (`Rahul Singh` / `rahul.singh` / about → **persisted to Postgres**), flipped last-seen→nobody + read-receipts→contacts (**privacy jsonb persisted**), Blocked(0) renders. server build+vet+profile curl-suite green (GET empty→PUT→GET confirms, dup username→409 USERNAME_TAKEN, block[204]→list→unblock[204]); web typecheck green. **DEFERRED:** avatar **image** upload (needs the media pipeline) + per-user **QR** (needs a QR renderer/dep). Mobile parity rides T5.01.
- [x] **T5.08  Contacts screen + sync** *(web done + verified live; mobile address-book read with T5.01)*
  PROMPT> address-book sync (hashed handles → /v1/contacts/sync), matched-contacts list, search-by-username, favorites, invite link. Both clients.
  DONE (web): wired the existing **`internal/contacts`** backend (T1.09 — peppered-HMAC discovery, PG-trigram username search, favorites, capability-token invites; all bearer-gated) to a new **Contacts screen** (`👥 Contacts` in the chat-list head). `AppServices` gained `searchContacts` (debounced, ≥2 chars), `listFavorites`/`addFavorite`/`removeFavorite`, `syncPhones` (paste numbers → matched list; 429→"4×/day" message), `createInvite`, and `openDirectWithUser` (shared 1:1 opener extracted from `startDirectChat`, so search/favorite/matched rows all "Message" straight into a thread). Screen sections: **Find people** (username search → rows with ☆/★ favorite-toggle + Message), **Favorites(n)** (starred, loaded from server), **Find by phone** (textarea → `POST /v1/contacts/sync` → matched), **Invite a friend** (create link + copy-to-clipboard). Server sends **plaintext handles over TLS** and peppers+hashes them server-side (only the HMAC is persisted). VERIFIED LIVE (two accounts — `rahul.singh` + seeded `alice_test`): searched "alice"→both alices, starred `alice_test` (**★ synced across search+favorites, persisted across nav**), Message→opened the 1:1 with the header showing "Alice Tester", phone-sync of 3 numbers→2 matched / 1 unregistered correctly excluded, invite link minted (`https://wa.local/i/…`). web typecheck green. **DEFERRED:** real mobile **address-book** read + hashed sync rides T5.01; invite **landing page** (resolve-token → sign-up) is a public-web route for later.
- [x] **T5.09  Groups UI** *(web done + verified live incl. real 2-account group delivery; @mentions + QR image deferred; mobile with T5.01)*
  PROMPT> create/edit group, member list + roles (owner/admin/member, promote/demote), add/remove/leave, invite links + QR join, announcements mode, @mentions, pinned. Both clients.
  DONE (web + a server fix). **Server fix (critical):** the groups backend (T1.01) wrote only `group_members`, but the chat accept/fan-out pipeline resolves recipients from `conversation_members` — so group sends would 403 `STATE_NOT_MEMBER` and deliver to nobody. Bridged in `groups/adapters/pg.go`: `CreateGroup` now also inserts the `conversations` row (id == group id, kind=1, group_id FK for ON-DELETE-CASCADE) and mirrors members into `conversation_members`; `AddMembers`/`RemoveMember`(=Leave)/`JoinViaInvite` keep the mirror in sync (helper `mirrorMembers`). **Client:** `AppServices` gained `createGroup`, `loadGroup`/`groupOf`/`groupNameOf`/`ensureConversationKind` (group-vs-direct classifier cache — note a recipient's inbox sets peerByConv even for groups, so only a 404 means direct), `listGroupMembers`, `addGroupMembers`/`removeGroupMember`/`setGroupRole`, `updateGroupInfo`/`setGroupSettings`, `leaveGroup`/`deleteGroup`, `createGroupInvite`/`joinGroup`. New **CreateGroup** screen (`＋👥 Group`): name + description + username-search member picker. New **GroupInfoScreen** (tap the thread header / ℹ️): role-gated settings (announcements toggle + who-can-post + who-can-edit-info), member list with role badges + promote/demote (owner) + remove (admin+), add-members search, invite link (create+copy), leave (all) / delete (owner). **Thread** now classifies the conversation: group name + ℹ️ in the header, and the composer is replaced with "📢 Only admins can post" when announcements/admin-only and I'm not an admin (server also enforces via `CanPost`). Chat-list rows show `👥 <group name>`. VERIFIED LIVE (two browsers, rahul.singh owner + alice_test): created "Weekend Trip", **messages fan out to both members' device inboxes** (confirmed in `message_inbox`) and render decrypted on the peer; **bidirectional** send/receive with ✓✓ receipts; group name in list+header; owner toggled **announcements** → member's composer disabled live (re-fetch on reopen); **promote** alice→admin persisted (role 1). server build+vet+groups tests green; web typecheck green. **DEFERRED:** @mentions autocomplete, per-user/**group QR image** (backend returns the invite URL string; needs a QR renderer/dep), pinned-in-group specifics (the pin action already exists from T5.05), member-list pagination beyond 256. Mobile parity rides T5.01.
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
