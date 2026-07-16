# Data Structures & Algorithms — Performance-Critical Choices

| Doc | The DS&A decisions that make the latency targets achievable |
|---|---|
| Status | v1.0 |
| Rule | Every choice here exists to hit an NFR. No cleverness without a number attached. |

## 1. Identifiers: UUIDv7 everywhere

**Problem:** UUIDv4 randomness destroys B-tree index locality — random-page writes at fan-out volume.
**Choice:** UUIDv7 (millisecond timestamp prefix + randomness). Time-ordered → inbox inserts append to the right edge of the index; range scans by recency are sequential I/O.
**Bonus:** client-generated, so the same ID serves as the idempotency key (NFR-15).

## 2. Message ordering: per-conversation monotonic sequence

**Problem:** total order across the system is expensive and unnecessary; users only perceive order *within* a conversation.
**Choice:** `conversations.seq` counter incremented in the accept transaction → each message gets `(conversation_id, seq)`. Clients render by seq, detect gaps (`seq > last+1`) and issue `sync.pull(cursor)`.
**Contention analysis:** hottest conceivable conversation ≈ tens of msg/s ≪ single-row update ceiling (~10k/s). Celebrity-group mitigation documented in HLD §20#10.

## 3. Inbox replay: cursor over a partitioned covering index

**Structure:** `message_inbox` partitioned by `(hash(recipient_device_id) % 16, month)`; covering index `(recipient_device_id, seq) INCLUDE (ciphertext, conversation_id, expires_at)`.
**Algorithm:** resume = single index-range scan `WHERE device=? AND seq>cursor ORDER BY seq LIMIT batch` — O(log n + k), no sort, no heap fetches. Delete-on-ACK keeps partitions small; month partitions make TTL purge a `DROP PARTITION` (O(1), no vacuum storm).

## 4. Fan-out: batched async writes off the ACK path

**Algorithm:** accept → durable-write sender's intent → ACK sender (perceived latency = 1 write) → worker expands membership (cached in Valkey, invalidated by group events) → `COPY`/multi-row INSERT in batches of 500 rows → publish per-device NATS subjects.
**Why not per-recipient sync inserts:** 1,024-member group = 1,024 round trips ≈ seconds; batched = 2–3 statements ≈ single-digit ms.
**Backpressure:** worker pool bounded; queue depth is an SLI; per-sender big-group rate limit upstream.

## 5. Connection registry (ws-gateway): sharded map + two-level routing

**In-pod:** `map[deviceID]*conn` sharded 256 ways by `deviceID % 256`, each shard with its own mutex — eliminates global-lock contention at 20k conns/pod with goroutine-per-connection reads.
**Cross-pod:** Valkey `route:{device} → pod_id` (TTL 90 s, heartbeat-refreshed) + per-device NATS subject. Any pod serves any device; no sticky sessions; a dead pod's routes expire automatically.

## 6. Dedupe: Valkey `SET NX EX` (deliberately not a Bloom filter)

`SET dedupe:{uuid} 1 NX EX 86400` — atomic, exact, one round trip. A Bloom filter would save memory but introduce false positives (dropped real messages — unacceptable) and add code. At 300 msg/s × 24 h ≈ 26 M keys ≈ ~2 GB worst case: affordable. **Correctness beats memory here.**

## 7. Rate limiting: GCRA in Valkey

**Choice:** GCRA (generic cell rate algorithm) via Lua — one key + one script call per check, smooth (no fixed-window boundary bursts), O(1) memory per limiter.
Applied tiers: per-IP (edge), per-device send rate, per-number OTP, per-sender group fan-out, per-user media quota.

## 8. PTT floor: fenced token + FIFO queue (single Lua script)

**Structures:** `ptt:{room}:floor = {device, fence, expiry}` + `ptt:{room}:queue` (list) + `ptt:{room}:fence` (monotonic counter).
**Acquire** (one atomic Lua exec): floor empty → take it, `fence = INCR`; else `RPUSH` queue, return position.
**Fencing:** every grant carries `fence`; SFU publish permission is granted for that fence only. A zombie ex-speaker resuming after partition holds a stale fence → its publish was already revoked → silence. Classic fenced-token pattern (Kleppmann) applied to media.
**Release/lapse:** TTL 2 s, heartbeat 500 ms; expiry pops next queue head. Grant latency budget: ~30 ms acquire + ~40 ms perm flip + ~80–100 ms first RTP ≈ **150–170 ms** ✅ NFR-08.

## 9. Receipt coalescing: per-conversation flush timers

**Problem:** naive receipts double frame volume.
**Algorithm:** accumulate `(conversation, kind, up-to-seq)` in a map; flush per conversation at most 1/250 ms; receipts are **cumulative** ("delivered ≤ seq N"), so one frame replaces dozens. Same watermark trick as TCP cumulative ACKs. Applied client- and server-side.

## 10. Presence: subscription sets, not broadcast

**Problem:** naive presence is O(contacts) fan-out per flap — a storm at 20k users.
**Choice:** clients subscribe only to the ~30 conversations on screen (`presence.sub` frame). Server keeps `presence_subs:{user}` sets in Valkey; a flap notifies subscribers only. Un-subscribed chats show last-known state, refreshed on open. Flap damping: offline announced only after 15 s grace.

## 11. Typing indicators: TTL keys, fire-and-forget

`SETEX typing:{conv}:{user} 5 1`, relayed only to online, subscribed members; never persisted, never queued for offline. Loss is harmless — next keystroke resends (1/3 s throttle).

## 12. Group membership cache: versioned sets

Fan-out needs member lists cheaply: Valkey `group_members:{id}` (set) + `group_ver:{id}` (int bumped by every membership event, which also invalidates). Fan-out worker compares cached version to event version; mismatch → reload from PG. Stale-read window is bounded by event ordering in NATS (per-group subject).

## 13. Delta sync: per-device cursors, server keeps no client state

Each device tracks `last_seq` per conversation locally; `sync.pull(cursor)` streams gap contents from inbox (§3 index). The server stores nothing about client sync progress except undelivered rows themselves — the ACK-delete **is** the cursor commit. Crash-safe: re-delivery is deduped by UUID client-side.

## 14. Client-side search: SQLite FTS5

BM25-ranked inverted index over decrypted local history (`messages_fts`, `porter unicode61` tokenizer, prefix indexes for as-you-type). Incremental via triggers on message insert/edit/delete. 100k-message store ≈ 30–60 MB index, query < 30 ms on mid-tier mobile — measured budget, not hope.

## 15. Complexity summary

| Hot operation | Cost | Bound |
|---|---|---|
| Accept + dedupe + seq | O(1) Valkey + 1 PG tx | NFR-05 |
| Fan-out N recipients | O(N/500) batched inserts, async | NFR-04 |
| Resume replay | O(log n + k) index scan | NFR-12 |
| Route lookup | O(1) Valkey GET | NFR-05 |
| Floor acquire | O(1) Lua | NFR-08 |
| Presence flap | O(subscribers) not O(contacts) | NFR-01 |
| Local search | O(query terms) FTS5 | NFR-10 |
