# Offline Synchronization & Local Store

| Doc | Client-side data layer: schema, outbox, delta sync, conflicts |
|---|---|
| Status | v1.0 · Shared design for mobile (SQLCipher) + web (SQLite-wasm/OPFS); implemented once in `clients/packages/sync-engine` |

## 1. Local schema (per device)

```sql
chats(conversation_id PK, kind, title, last_seq_seen, last_msg_preview, unread_count,
      muted_until, pinned, draft)
messages(msg_uuid PK, conversation_id, seq, sender, kind, body, media_ref,
         state,           -- outgoing: sending|sent|delivered|read ; incoming: received
         edited_of, deleted, reactions_json, starred, pinned, created_at)
messages_fts(FTS5 content=messages: body)          -- porter unicode61, prefix indexes
attachments(media_ref PK, object_key, file_key, hash, local_path, download_state, size)
outbox(client_ref PK, frame_blob, conversation_id, attempts, next_retry_at, created_at)
contacts(user_id PK, username, display_name, avatar_ref, favorite, blocked)
signal_sessions(address PK, record BLOB)           -- libsignal-managed
identity_keys(user_id, device_id, key, verified)   -- + own keys in hardware keystore
sync_cursors(conversation_id PK, last_seq)         -- authoritative replay position
settings(key PK, value)
```

## 2. Outbox algorithm (effectively-once from the client side)

```
send: write messages(state=sending) + outbox row in ONE local tx  → optimistic UI
flush loop (connectivity-aware):
  oldest outbox row → send frame (client_ref = msg_uuid)
  MsgAck → local tx: messages.state=sent(seq), delete outbox row
  Error(retryable) → attempts++, next_retry = backoff(attempts, cap 30 s)
  Error(permanent) → state=failed + user retry affordance
restart/crash → outbox persists → resend (server dedupe absorbs)
```

Invariant: UI state derives **only** from the local DB; the network layer mutates DB, never UI (single writer of truth on the client too).

## 3. Delta sync (incoming)

On connect: send cursors from `sync_cursors` → server replays gaps (InboxBatch) → client per-batch local tx: insert messages (dedupe by msg_uuid) → advance cursor → **then** ClientAck (ACK-after-persist contract, websocket-protocol §3). Gap detection during live traffic (seq jump) → `SyncPull` for that conversation only. Overlays (edit/delete/reaction) apply idempotently by (target, kind, editor, ts) — an overlay before its target parks in a pending table (rare: fan-out reordering across conversations is impossible; within a conversation seq forbids it — this handles cross-device restore edges).

## 4. Conflict resolution (deliberately boring)

| Case | Rule |
|---|---|
| Same message from 2 paths (resume + live) | msg_uuid PK → idempotent insert |
| Edit vs delete race | delete wins (tombstone terminal) |
| Reactions from many devices | set-union keyed (reactor, emoji); removal = tombstone pair |
| Read state across own devices | max(read_seq) monotonic merge (self-sync messages) |
| Draft across devices | last-writer-wins, device-local preference (drafts don't sync in V2) |

No CRDTs needed: server seq gives per-conversation total order; only commutative metadata merges remain.

## 5. Media offline

Downloads resumable (ranged GETs on ciphertext); auto-download rules per network type (wifi/cellular/never, per media class); local media GC by LRU when storage pressure (attachments row keeps envelope key — re-download possible while server TTL lives; after that, media exists only where downloaded).

## 6. Multi-device bootstrap & backup restore

New linked device: E2E-encrypted history transfer primary→new (sequence-diagrams §8) — chunked, resumable, progress UI; then normal delta sync takes over. Backup restore: decrypt archive (Argon2id key) → import → rebuild FTS + cursors → full re-sync of the ≤30-day server window. Both paths converge on identical local state (property-tested in sync-engine suite).
