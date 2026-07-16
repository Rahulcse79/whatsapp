# End-to-End Encryption Design

| Doc | libsignal integration: sessions, groups, media, calls, verification, multi-device |
|---|---|
| Status | v1.0 |
| Iron rule | **We never roll our own crypto.** libsignal (official Rust core + platform bindings) for all primitives and protocols. This doc specifies *integration*, not cryptography. |

## 1. Key inventory (who holds what)

| Key | Where it lives | Server sees? |
|---|---|---|
| Identity key pair (per device) | Device keystore (Secure Enclave / Android Keystore / IndexedDB+WebCrypto wrapped) | Public only |
| Signed prekey + one-time prekeys | Generated on device | Public only (distribution — DB doc §2) |
| Session state (Double Ratchet chains) | Device SQLCipher `signal_sessions` | Never |
| Group Sender Keys | Per member device, distributed pairwise | Never |
| Per-file media keys | Random per file, inside E2EE envelope | Never |
| Per-story keys | Distributed pairwise to audience snapshot | Never |
| Call frame keys (SFrame) | Derived from Signal sessions, epoch-rotated | Never |
| Backup key | Argon2id(passphrase) or 64-digit recovery key | Never |

## 2. Pairwise sessions: X3DH + Double Ratchet

- Session establishment: fetch recipient bundle (`GET /keys/bundle/{user}` — one bundle **per device**), X3DH with one-time prekey consumption; first message carries the X3DH header.
- Double Ratchet per (sender device → recipient device) pair: forward secrecy (compromise reveals nothing prior) + post-compromise security (healing on next round trip).
- Multi-device fan-out: sender encrypts **per recipient device** (N sessions), plus **sync copies to its own other devices** (self-sessions) — this is how a user's other devices see sent messages.

## 3. Groups: Sender Keys (HLD §8.3)

- Per (group, sender device): a Sender Key chain; distributed to every member device via existing pairwise sessions when joining/rotating.
- Send = encrypt once with own Sender Key → server fans ciphertext (bandwidth-efficient at 1,024 members).
- **Rotation on membership change:** member removed ⇒ all members generate fresh Sender Keys (removed member can't read forward). Trigger = ordered `group.events` (server signals, clients execute; server never touches key material). Member added ⇒ existing keys distributed to the new device set (no back-history: inbox only holds ≤ 30-day undelivered anyway, and new members receive nothing before join seq).
- MLS is the V3 watch item for very large groups (tree-based key agreement); revisit trigger in ADR backlog.

## 4. Media & stories

Media: per-file random key + AES-256-GCM (chunked); key + content hash travel inside the message envelope; blob is opaque to server/CDN forever. Stories: same, with per-story key distributed pairwise to the audience snapshot at post time (audience changes don't retro-apply — matches WhatsApp semantics).

## 5. Multi-device trust: signed device lists

- The **primary device** signs the user's device list (add/remove) with its identity key; linked devices present certs chained to it (`devices.cert`).
- Clients verify: messages from user X must come from a device on X's signed list. A malicious server inserting a device forces a *visible* device-list change ⇒ contacts holding X's safety number see a key-change warning.
- Device link flow: QR carries link token + new device pubkey; primary approves + signs; history bootstrap is E2E-encrypted primary→new device (server relays ciphertext chunks — sequence diagram §8).

## 6. Verification UX

Safety numbers (per contact pair) = hash of both identity keys; QR + numeric compare; key-change events surface persistently in chat (dismissible per policy, never silently). Verified state stored locally; resets on identity-key change only (not device adds under a valid signed list).

## 7. Calls

DTLS-SRTP transport encryption + SFrame-style frame encryption over insertable streams; frame-key epochs bumped on join/leave (call-ctl signals epoch, keys derive from pairwise sessions). SFU forwards opaque frames ([rtc-lld.md](../05-services/rtc-lld.md) §2).

## 8. What E2EE forbids (enforcement list — checked at design review)

Server-side: content search, content moderation/scanning, thumbnails, transcoding, link previews, message translation, server call recording, content analytics, "AI on messages". Every one has a designed client-side or consent-based alternative (HLD corrections #4/#6/#9; §2.1; Appendix D).

## 9. Residual risks (honest register)

| Risk | Status |
|---|---|
| Endpoint compromise (malware on device) | Out of E2EE scope by definition; mitigations: keystore-backed keys, SQLCipher, screen-lock reauth |
| Metadata (who↔whom, when, sizes) | Server necessarily sees routing metadata; minimized per retention table; sealed-sender is a V3 study item |
| libsignal AGPL | Legal posture confirmed for service use (HLD §24); track upstream |
| Web client key storage | Weakest platform (no hardware enclave); documented, PWA still uses WebCrypto non-extractable keys |
