# UAT Matrix — FR × Acceptance Criteria

| Doc | User-acceptance checklist: every functional requirement → its acceptance criterion → how it is verified → status |
|---|---|
| Source | [functional-requirements.md](functional-requirements.md) (65 FRs) · AC convention [user-stories-personas.md](user-stories-personas.md) §2 (Given/When/Then, every AC automatable) |
| Generated | T4.09 |

**Status legend** — `CI`: implemented and asserted by an automated test in CI ·
`staging`: needs an end-to-end run on the staging stack (behind GATE P0/P2/P4,
which are infra-gated) · `device`: needs an on-device pass (iOS/Android hardware)
· `manual`: UAT-script / human verification. A single FR can carry more than one.

## AUTH — Identity & Authentication

| FR | Acceptance criterion | Verify | Status |
|---|---|---|---|
| FR-AUTH-01 | Given a valid phone (or email in offline profile), when I request an OTP, then a challenge is created and the response is identical whether the number exists (no enumeration). | `auth` service + adapter tests | CI + staging |
| FR-AUTH-02 | Given a challenge, when I submit a 6-digit code, then it verifies within 10 min / ≤ 5 attempts; a 6th attempt or 4th/day is rate-limited. | `auth/domain` VerifyCode + limiter tests | CI |
| FR-AUTH-03 | Given a 2FA PIN is set, when I re-register, then the PIN gate (Argon2id) must pass before tokens issue. | `auth` VerifyPIN test | CI |
| FR-AUTH-04 | Given a refresh token, when I rotate it, then a superseded token's reuse kills the whole session. | `auth` Refresh reuse-detection test | CI |
| FR-AUTH-05 | Given a primary device, when a new device shows its QR, then linking is approved + signed by the primary identity key; ≤ 4 linked. | devices link tests + `deviceList`/`qrLink` (T3.01) | CI + device |
| FR-AUTH-06 | Given a device, when I revoke it, then tokens + prekeys + push route are atomically invalidated. | devices atomic-revoke tx test | CI |
| FR-AUTH-07 | Given multiple sessions, when I log out one device, then only that session ends and the list reflects it. | devices/session tests | CI + manual |
| FR-AUTH-08 | Given my account, when I delete it, then it tombstones immediately and purges ≤ 30 d; export produces an encrypted archive. | account delete/export path | staging + manual |

## USER — Profile & Privacy

| FR | Acceptance criterion | Verify | Status |
|---|---|---|---|
| FR-USER-01 | Profile CRUD; a taken username returns `VALIDATION_USERNAME_TAKEN`. | users API test | CI + manual |
| FR-USER-02 | Each privacy field (last-seen/avatar/about/read-receipts) honours everyone/contacts/nobody, enforced server-side at presence-sub + receipt paths. | presence privacy + receipt gate tests | CI + staging |
| FR-USER-03 | Given I block a user, then they see no presence and cannot message or call me. | block semantics test | CI + manual |
| FR-USER-04 | Given my user QR, when scanned, then the scanner can add me as a contact. | qr add path | manual |

## CONT — Contacts

| FR | Acceptance criterion | Verify | Status |
|---|---|---|---|
| FR-CONT-01 | Given my address book, when I sync, then only peppered HMACs are sent/stored (no plaintext), ≤ 5k/call, 4/day, uniform timing. | contacts Sync tests (T1.09) | CI |
| FR-CONT-02 | Given a username query ≥ 2 chars, when I search, then metadata-only matches return (caller excluded), rate-limited. | contacts Search tests | CI |
| FR-CONT-03 | Favorite/unfavorite (by user id); an invite link resolves/expires/revokes. | contacts favorites + invite tests | CI |

## MSG — 1:1 Messaging

| FR | Acceptance criterion | Verify | Status |
|---|---|---|---|
| FR-MSG-01 | Given both online, when I send, then delivery is exactly-once and ticks progress sending→sent→delivered→read. | chat accept + gateway P1 scenario | CI + staging |
| FR-MSG-02 | Receipts are batched (N→1 per conv/kind); read receipts suppressed when privacy = nobody. | receipt coalescer tests | CI |
| FR-MSG-03 | Typing throttled ≤ 1/3 s; presence/last-seen per privacy. | presence/typing tests | CI + staging |
| FR-MSG-04 | Reply/forward/copy preserve content and reference the original. | client message ops | manual |
| FR-MSG-05 | Delete-for-everyone within 48 h applies as a tombstone; server validates sender + window only. | overlay window boundary tests (T0.21) | CI |
| FR-MSG-06 | Edit within 15 min applies as an overlay; past the window is rejected. | overlay window tests | CI |
| FR-MSG-07 | Pin/star (local) and set-union emoji reactions apply and toggle. | conflict + repo pin/star tests | CI |
| FR-MSG-08 | Given a URL, when I send, then a preview is generated on MY device and carried in the envelope (recipient fetches nothing). | linkPreview + messageBody tests (T1.11) | CI |
| FR-MSG-09 | Given the recipient offline, when I send, then the ciphertext holds ≤ 30 d and delivers exactly once on reconnect — zero loss after ACK. | P2 offline→resume + inbox-soak audit | staging |
| FR-MSG-10 | Full-text search over local decrypted history returns ranked hits offline. | client-core FTS5 + search tests (T1.10) | CI |

## GRP — Groups

| FR | Acceptance criterion | Verify | Status |
|---|---|---|---|
| FR-GRP-01 | Create a group ≤ 1,024; > 1,024 is rejected. | groups service tests | CI |
| FR-GRP-02 | Owner/admin/member roles; permission matrix enforced (admin can't remove admin, owner unremovable). | groups roles matrix tests | CI |
| FR-GRP-03 | Add/remove/leave/delete with version bump + events; owner-must-transfer on leave. | groups service tests | CI |
| FR-GRP-04 | Invite link + QR join; revocation invalidates the link. | groups invite/join tests | CI |
| FR-GRP-05 | who-can-post / edit-info / announcements-mode enforced. | groups settings tests | CI |
| FR-GRP-06 | @mentions, pins, and aggregate (all-members) receipts flip exactly once. | fanout aggregate-receipts tests (T1.03) | CI + staging |
| FR-GRP-07 | Sender-Key encryption; membership change rotates keys before the next message. | senderKey rotation + P6 (T1.02/T1.12) | CI |

## MED — Media

| FR | Acceptance criterion | Verify | Status |
|---|---|---|---|
| FR-MED-01 | Send image/video/audio/voice/doc ≤ 25 MB, multi-file; > 25 MB rejected. | media domain/upload tests | CI |
| FR-MED-02 | Compression + thumbnail/blurhash are client-side. | media-pipeline (compress/thumb = platform seam) | CI + device |
| FR-MED-03 | AES-256-GCM client-side before upload; server stores ciphertext only. | mediaCrypto tests (T1.05) | CI |
| FR-MED-04 | Resumable multipart upload re-presigns only missing parts; progress + retry. | uploader P13 tests | CI + staging |
| FR-MED-05 | GIF search is proxied server-side (client IP never reaches provider); sticker packs install. | gif/tenor + stickers tests (T1.07) | CI |
| FR-MED-06 | In-chat playback/preview + download manager work. | media UX (T1.06) | manual + device |

## CALL — Voice & Video

| FR | Acceptance criterion | Verify | Status |
|---|---|---|---|
| FR-CALL-01 | 1:1 + group ≤ 32 with E2EE frames (SFrame); the SFU forwards ciphertext it can't read. | frameCrypto + groupCallCrypto tests (T2.03/T2.07) | CI + staging |
| FR-CALL-02 | Lock-screen ring via CallKit / ConnectionService + VoIP push. | ringBridge tests (T2.04) | device |
| FR-CALL-03 | Ring machine ringing/answered/declined/busy/missed(45 s); answer-elsewhere to siblings. | ring matrix P12 (T2.02/T2.10) | CI |
| FR-CALL-04 | Mute/speaker/camera-switch/screen-share/on-device blur — blur never server-side. | camera/screenShare/videoEffects tests (T2.05/T2.06) | CI + device |
| FR-CALL-05 | Simulcast + video→audio downgrade + ICE restart < 5 s. | simulcast/quality/iceRecovery tests (T2.05/T2.09) | CI + staging |
| FR-CALL-06 | Call history (metadata only, 90 d) + missed-call notifications. | calls history + purge tests (T2.08) | CI |
| FR-CALL-07 | Recording is client-side with a mandatory consent signal only. | design (HLD §10.6) | manual |

## PTT — Push-to-Talk

| FR | Acceptance criterion | Verify | Status |
|---|---|---|---|
| FR-PTT-01 | Room ≤ 500 listeners; one speaker; press-and-hold floor. | ptt service tests (T3.04) | CI + staging |
| FR-PTT-02 | Atomic acquire + fencing + FIFO queue with position. | ptt Lua acquire + P11 tests | CI |
| FR-PTT-03 | Floor grant p95 ≤ 200 ms; 2×500 ms heartbeat lapse auto-releases; 60 s max. | ptt tests + `ptt.js` load (GATE P3) | CI + staging |
| FR-PTT-04 | Grant flips SFU publish permission for that fence; stale-fence audio is dead. | ptt SFU perm-flip seam | CI + staging |

## STORY — Stories/Status

| FR | Acceptance criterion | Verify | Status |
|---|---|---|---|
| FR-STORY-01 | Photo/video/text story; hard 24 h expiry purge. | stories expiry tests (T3.05) | CI |
| FR-STORY-02 | Audience evaluated at post time (later changes don't retro-apply); per-story key. | stories audience-snapshot tests | CI |
| FR-STORY-03 | View receipts + reactions to author; viewer list. | stories viewers tests | CI + manual |

## NOTIF — Notifications

| FR | Acceptance criterion | Verify | Status |
|---|---|---|---|
| FR-NOTIF-01 | Data-only push (zero plaintext through FCM/APNs); client fetches + decrypts + renders. | notify pipeline tests (T0.16) | CI + device |
| FR-NOTIF-02 | VoIP push for calls; mention elevation client-side post-decrypt. | voipPush + RingBridge (T2.04) | device |
| FR-NOTIF-03 | Per-chat + global mute; badge counts computed on-device. | client notif settings | manual + device |
| FR-NOTIF-04 | Offline profile uses UnifiedPush/ntfy. | ntfy driver (T0.16) | staging (offline) |

## SYNC — Multi-Device & Offline

| FR | Acceptance criterion | Verify | Status |
|---|---|---|---|
| FR-SYNC-01 | Per-device Signal sessions; a message is sealed once per recipient device + self-sync copies. | E2EEEngine fan-out tests (T3.03) | CI |
| FR-SYNC-02 | History bootstrap to a new linked device is an E2EE, QR-authenticated, resumable transfer. | sync-engine bootstrap tests (T3.02) | CI + device |
| FR-SYNC-03 | Outbox retries with the same UUIDv7; cursor delta sync heals gaps; no loss/dupe. | outbox property test + cursors (T0.17) | CI |
| FR-SYNC-04 | Encrypted client backups (Argon2id key); the server cannot read them. | backup crypto tests (T3.06) | CI |

## ADMIN — Console, Trust & Safety

| FR | Acceptance criterion | Verify | Status |
|---|---|---|---|
| FR-ADMIN-01 | OIDC SSO + RBAC (viewer/agent/operator/owner); IP allowlist. | admin OIDC + RBAC tests (T4.01) | CI + staging |
| FR-ADMIN-02 | Metadata search + report queue + warn/suspend/ban — never content access. | admin service tests | CI |
| FR-ADMIN-03 | Every admin action appends to an immutable audit_log in the same tx. | admin audit-in-tx tests | CI |
| FR-ADMIN-04 | Feature-flag management + kill-switches + metadata-only dashboards. | flags (T4.02) + analytics (T4.03) tests | CI |
| FR-ADMIN-05 | A report attaches ciphertext only with the reporter's consent. | report consent path | CI + manual |

## Accessibility (WCAG 2.1 AA — the axe half of T4.09)

| Check | Where | Status |
|---|---|---|
| Every form control has a label (login/verify/search/composer aria-labels; not placeholder-only). | web `screens.tsx` | fixed |
| Interactive rows are focusable + keyboard-operable (Enter/Space), not click-only `<li>`. | web `screens.tsx` (`onActivate`) | fixed |
| Icon-only controls have accessible names (`aria-label`); decorative glyphs `aria-hidden`. | web screens / media / call overlay | fixed |
| Images inside labelled buttons use empty `alt`; content images (gallery) use meaningful `alt`. | web `MediaMessage`, `Gallery` | ok |
| Dialogs use `role="dialog" aria-modal`; decorative canvases `aria-hidden`. | web `Gallery`, `BlurhashCanvas` | ok |
| Document `lang`, viewport, title, theme-color. | web `index.html` | ok |
| Mobile: every `Pressable` control carries `accessibilityLabel`. | RN screens | audit ongoing |

Run `axe`/Lighthouse against the built PWA per release; RN uses the accessibility
inspector. Remaining device-tier checks (focus order under a screen reader,
contrast on live theme) are part of the on-device UAT pass.
