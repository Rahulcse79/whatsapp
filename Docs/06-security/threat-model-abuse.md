# Threat Model & Anti-Abuse

| Doc | STRIDE-organized threat register + abuse controls |
|---|---|
| Status | v1.0 · Live document: every new feature adds its row before implementation |

## 1. Assets

User content (highest — protected structurally by E2EE) · social graph/metadata · identity (phone/username) · availability of the service · SMS/infra spend · admin plane.

## 2. Threat register (STRIDE × asset)

| # | Threat | Class | Mitigation | Residual |
|---|---|---|---|---|
| T1 | Full server compromise reads chats | Info disclosure | Relay model: attacker gets undelivered ciphertext + metadata only; keys on devices | Metadata exposure — minimized retention |
| T2 | MITM at network edge | Tampering | TLS 1.3 + mobile pinning; safety numbers catch key substitution | User skips verification |
| T3 | Malicious server injects a device | Spoofing/Elevation | Primary-signed device lists; visible roster; key-change warnings ([e2ee-design.md](e2ee-design.md) §5) | Social-engineered approval |
| T4 | SIM swap re-registration | Spoofing | 2FA PIN (Argon2id, hard rate limit); re-registration alerts to all devices | PIN-less accounts (UX pushes enrollment) |
| T5 | Stolen unlocked phone | Info disclosure | SQLCipher + OS lock; remote device revocation; screen-lock reauth for app | Fundamental endpoint limit |
| T6 | Replay/duplication of frames | Tampering | Ratchet nonces + server dedupe + per-conv seq | — |
| T7 | Push-channel content snooping | Info disclosure | Data-only pushes — zero content via FCM/APNs | Traffic-timing analysis |
| T8 | Presigned URL leakage | Info disclosure | Short TTL; blobs are ciphertext anyway | — |
| T9 | OTP pumping / SMS toll fraud | DoS(spend) | 3/hr,5/day per number; per-IP/ASN caps; attestation (online profile); **provider spend circuit-breaker** | Cost cap, not zero |
| T10 | Spam floods | DoS/abuse | Per-device GCRA send limits; graduated new-account limits; big-group throttles; metadata-only heuristics (fan-out patterns, contact-add velocity) | Content-blind by design — heuristics only |
| T11 | Contact-sync enumeration | Info disclosure | Peppered HMAC discovery; 4/day + volume caps; response padding (found/not-found timing-equal); PSI in V3 | Bulk enumeration slowed, not eliminated |
| T12 | Group-invite scraping | Info disclosure | Tokens are capability URLs: expiry, max-uses, revocation | Link sharing is user-controlled |
| T13 | WS connection exhaustion | DoS | Edge per-IP caps; per-conn frame quotas; auth-before-registry; 3-pod blast radius; jittered client backoff | Volumetric DDoS → upstream scrubbing (deployment-specific) |
| T14 | Inbox-flood storage attack (send to offline users) | DoS(storage) | Per-sender rate limits; per-recipient-inbox depth cap (drop-oldest overlay events first, alert); 30-d TTL | — |
| T15 | Malicious media (steganography-borne malware) | Tampering | Server can't scan (ciphertext) — client-side: type allowlist, size caps, no auto-execute, OS sandbox rendering, download warnings | Endpoint AV is out of scope |
| T16 | Admin insider abuse | Elevation | No content access exists; RBAC least privilege; append-only audit log; quarterly audit review | Metadata visible to T&S role |
| T17 | Abuse reporting weaponized (false reports) | Abuse | Reports carry reporter-consented ciphertext only; T&S reviews with account-history context; actions appealable | Human process |
| T18 | Zombie PTT speaker after partition | Tampering | Fencing tokens at SFU permission layer (DS&A §8) | — |
| T19 | Supply-chain compromise | Tampering | Signed images, SBOM, pinned deps, govulncheck, admission policy | Upstream zero-days |
| T20 | Air-gap profile: private CA compromise | Spoofing | step-ca HSM/offline root, short-lived intermediates; CA rotation runbook | Operator competence |

## 3. Abuse-control parameters (single source; flags-tunable)

| Control | Value |
|---|---|
| OTP per number / IP | 3/hr + 5/day / 10/day |
| New account: max messages day-1 / new-conversation rate | 200 / 20 distinct recipients/day, graduating over 7 days |
| Send rate per device | 20 msg/s burst 40 |
| Group sends ≥ 256 members | 2/s per sender |
| Contact sync | 4/day, ≤ 5k hashes |
| Report queue SLA | T&S review < 24 h (business) |
| Inbox depth per recipient device | 50k rows soft cap → alert + sender backpressure |

## 4. Review protocol

New feature ⇒ (1) new rows here, (2) E2EE-forbidden check (e2ee-design §8), (3) rate-limit line in api-standards §4. Quarterly: re-read the register against incidents; annual external pentest (P4 first).
