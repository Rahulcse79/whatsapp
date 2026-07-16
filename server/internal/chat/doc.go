// Package chat owns the message hot path: accept (dedupe, per-conversation
// sequencing), inbox fan-out, receipts, and overlay events (edit/delete/
// react/pin). Target: < 10 ms server time per accept.
//
// The server relays ciphertext; nothing in this package may inspect content.
// Design: Docs/05-services/core-api-lld.md §2–3,
// Docs/02-architecture/data-structures-algorithms.md §1–4, §6, §9.
package chat
