# ADR-001: Relay Model — No Server-Side Message Archive

Status: **Accepted** · Date: 2026-07-14 · Upstream: HLD correction #5, §7.2

## Context

The raw requirements implied a server-side message store (and Telegram-style cloud history). Under E2EE the server only ever holds ciphertext, so an archive provides no product features (no server search, no server preview) while creating maximal liability: unbounded storage growth, a breach target, and legal exposure.

## Decision

The server persists a message **only until every recipient device ACKs it, or 30 days, whichever comes first** (`message_inbox`, delete-on-ACK). Clients are the system of record for history. Optional backups are client-encrypted blobs (Argon2id-derived key) the server cannot read.

## Alternatives

- **Full server archive (Telegram model):** requires abandoning default E2EE. Rejected — privacy is the product (see product-vision.md).
- **Encrypted server archive with user keys:** server stores ciphertext forever; storage grows unboundedly for data the server can't use; complicates deletes. Rejected as cost without benefit — client backups achieve the same user outcome.

## Consequences

- ✅ ~100× less server storage; breach yields undelivered ciphertext only; GDPR deletes are trivial.
- ⚠️ Losing all devices + backup key = history gone. Mitigation: aggressive backup UX; clearly-worded recovery-key flow.
- ⚠️ Multi-device history bootstrap must come from the primary device or backup, not the server (designed: HLD §7.3).
- Revisit trigger: none anticipated; this is load-bearing for the whole design.
