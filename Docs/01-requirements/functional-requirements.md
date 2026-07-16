# Functional Requirements

| Doc | Functional Requirements (FR) — complete, numbered |
|---|---|
| Status | v1.0 |
| Upstream | [/HLD.md](../../HLD.md) §2.1 |
| Convention | FR-`<domain>`-`<nn>`. Every FR is testable; acceptance criteria live with user stories. **[E2EE]** tags mark requirements whose design is constrained by end-to-end encryption. |

## AUTH — Identity & Authentication

- **FR-AUTH-01** Register with phone number + SMS OTP (offline profile: email OTP / TOTP — HLD §17.5).
- **FR-AUTH-02** OTP verification: 6 digits, 10-min validity, max 5 attempts, rate limits 3/hr & 5/day per number.
- **FR-AUTH-03** Optional 2FA registration PIN (Argon2id-hashed) required on re-registration.
- **FR-AUTH-04** Access JWT (10 min) + rotating refresh token bound to device; reuse detection kills the session.
- **FR-AUTH-05** Device registry: 1 primary + ≤ 4 linked devices; QR-based linking signed by primary identity key. [E2EE]
- **FR-AUTH-06** List / rename / revoke devices; revocation atomically invalidates tokens, prekeys, push routes.
- **FR-AUTH-07** Logout (per device), session listing.
- **FR-AUTH-08** Account deletion: immediate tombstone, full purge ≤ 30 days; account export (GDPR).

## USER — Profile & Privacy

- **FR-USER-01** Profile: unique username, display name, avatar, about text.
- **FR-USER-02** Privacy settings per field: last-seen/online, avatar, about, read receipts — audiences: everyone / contacts / nobody.
- **FR-USER-03** Block/unblock; blocked users see no presence, cannot message or call.
- **FR-USER-04** Per-user QR code to add a contact.

## CONT — Contacts

- **FR-CONT-01** Address-book sync via hashed phone numbers; server never stores plaintext address books. [E2EE-adjacent]
- **FR-CONT-02** Search users by username (server, metadata only).
- **FR-CONT-03** Favorites; invite-a-friend share link.

## MSG — 1:1 Messaging

- **FR-MSG-01** Send/receive E2EE text messages; states sending → sent → delivered → read. [E2EE]
- **FR-MSG-02** Delivery/read receipts, batched; read receipts privacy-gated by FR-USER-02.
- **FR-MSG-03** Typing indicator (throttled 1 per 3 s); online/last-seen per privacy settings.
- **FR-MSG-04** Reply (quote), forward, copy.
- **FR-MSG-05** Delete-for-me; delete-for-everyone within 48 h. Overlay event; server validates sender + window only. [E2EE]
- **FR-MSG-06** Edit within 15 min (overlay event). [E2EE]
- **FR-MSG-07** Pin, star, emoji reactions.
- **FR-MSG-08** Link previews generated on the **sender's** device, carried in the envelope. [E2EE]
- **FR-MSG-09** Offline recipients: message held encrypted server-side ≤ 30 days, delivered on reconnect; zero loss after server ACK.
- **FR-MSG-10** Client-side full-text search over local history (SQLite FTS5). [E2EE]

## GRP — Groups

- **FR-GRP-01** Create group ≤ 1,024 members; name, description, avatar.
- **FR-GRP-02** Roles: owner, admin, member; promote/demote; multi-admin.
- **FR-GRP-03** Add/remove members; leave; delete group.
- **FR-GRP-04** Invite links + QR join; link revocation.
- **FR-GRP-05** Permissions: who can post / edit info; announcements mode (admins post only).
- **FR-GRP-06** @mentions; pinned messages; aggregate receipts (all-members semantics).
- **FR-GRP-07** Sender-Keys group encryption; key rotation on membership change. [E2EE]

## MED — Media

- **FR-MED-01** Send images, video, audio, voice notes, documents (PDF/ZIP/Office) ≤ 25 MB; multi-file.
- **FR-MED-02** Client-side compression (WebP/AVIF, ≤ 720p H.264, Opus) + thumbnail/blurhash generation. [E2EE]
- **FR-MED-03** Client-side AES-256-GCM encryption before upload; server stores ciphertext only. [E2EE]
- **FR-MED-04** Resumable chunked uploads (multipart presigned); progress UI; retry.
- **FR-MED-05** GIFs & sticker packs; GIF search proxied server-side (client IP never reaches provider).
- **FR-MED-06** In-chat playback/preview; download management.

## CALL — Voice & Video

- **FR-CALL-01** 1:1 and group calls ≤ 32 (voice + video); E2EE frames. [E2EE]
- **FR-CALL-02** Lock-screen ringing: APNs PushKit/CallKit, FCM high-priority/ConnectionService.
- **FR-CALL-03** Ring state machine: ringing/answered/declined/busy/missed (45 s timeout).
- **FR-CALL-04** Mute, speaker, Bluetooth, camera switch, screen share, on-device background blur. [E2EE]
- **FR-CALL-05** Adaptive quality (simulcast); auto video→audio downgrade; ICE restart < 5 s on network change.
- **FR-CALL-06** Call history (metadata only, 90 days); missed-call notifications.
- **FR-CALL-07** Recording: client-side with mandatory consent signaling only (HLD §10.6).

## PTT — Push-to-Talk

- **FR-PTT-01** Audio rooms ≤ 500 listeners; one speaker at a time; press-and-hold floor.
- **FR-PTT-02** Server-authoritative floor: atomic acquire, fencing sequence, FIFO queue with position feedback.
- **FR-PTT-03** Floor grant p95 ≤ 200 ms; heartbeat lapse (2 × 500 ms) auto-releases; max speak duration 60 s.
- **FR-PTT-04** Media-plane enforcement: publish permission flipped via SFU API. [E2EE]

## STORY — Stories/Status

- **FR-STORY-01** Photo/video/text stories; 24 h hard expiry.
- **FR-STORY-02** Audience controls evaluated at post time; per-story key distributed via Signal sessions. [E2EE]
- **FR-STORY-03** View receipts + reactions (E2EE events to author); viewer list.

## NOTIF — Notifications

- **FR-NOTIF-01** Data-only pushes — zero plaintext through FCM/APNs; client fetches, decrypts, renders. [E2EE]
- **FR-NOTIF-02** VoIP push for calls; mention elevation (client-side, post-decryption).
- **FR-NOTIF-03** Per-chat & global mute; notification settings; badge counts computed on-device.
- **FR-NOTIF-04** Offline profile: UnifiedPush/ntfy driver (HLD §17.5).

## SYNC — Multi-Device & Offline

- **FR-SYNC-01** Per-device Signal sessions; messages encrypted per recipient device. [E2EE]
- **FR-SYNC-02** History bootstrap to a new linked device: encrypted transfer from primary (QR-authenticated).
- **FR-SYNC-03** Offline queue (outbox) with retry; cursor-based delta sync on reconnect; no loss, no duplication (UUIDv7 dedupe).
- **FR-SYNC-04** Encrypted client-side backups (passphrase/recovery-key derived, Argon2id); server cannot read them. [E2EE]

## ADMIN — Console, Trust & Safety (HLD §15.6)

- **FR-ADMIN-01** SSO(OIDC)+2FA admin SPA; RBAC viewer/T&S/operator/owner; IP allowlist.
- **FR-ADMIN-02** User metadata search; report queue; actions warn/suspend/ban — never content access. [E2EE]
- **FR-ADMIN-03** Every admin action appends to immutable `audit_log`.
- **FR-ADMIN-04** Feature-flag management; server config; aggregate dashboards (metadata only).
- **FR-ADMIN-05** User reports attach ciphertext **only with reporter's consent** (WhatsApp model).

## Requirements that were corrected (do not re-add)

| Rejected as specified | Why | Corrected form |
|---|---|---|
| Server-side message search | Server holds ciphertext | FR-MSG-10 client-side FTS |
| Server-side thumbnails/compression/media scan | Same | FR-MED-02 client-side |
| Server-side call recording | Same | FR-CALL-07 client-side + consent |
| Push message previews from server | Same | FR-NOTIF-01 data-only push |
| Server-generated link previews | Same | FR-MSG-08 sender-side |
