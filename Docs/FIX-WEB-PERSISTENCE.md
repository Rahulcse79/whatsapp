# Chats vanish on refresh: analysis and fix plan

**Symptom:** reload the web app and every conversation and message is gone.

## Root cause

The web client's message store is held **in memory**. It is not a bug hiding in
a corner — the worker says so on line 3:

```ts
// clients/web/src/worker/db.worker.ts
// The shell uses the in-memory MemoryMessageRepo (OPFS SQLite-wasm lands with
// the persistence epic) …
const repo = new MemoryMessageRepo();
```

`MemoryMessageRepo` is a `Map` in a worker. Closing or reloading the tab tears
the worker down and takes every conversation, message and cursor with it. The
persistence epic it points at was never done, so the placeholder has been the
shipping behaviour.

## Why the server does not save your chats either

This is the part worth being precise about, because "save it in the database" is
ambiguous here — there are two databases and only one of them is meant to hold
history.

**The server deliberately keeps no chat archive.** `message_inbox` is a *relay
buffer*: `AckDelivered` issues `DELETE FROM message_inbox … WHERE seq <= $3` the
moment a device confirms receipt, with a 30-day TTL only as a backstop for
devices that never come back. That is [ADR-001](02-architecture/adr/ADR-001-relay-model.md)
(relay model, no server-side message archive) and it is the privacy property the
whole product is built on — the server stores ciphertext it cannot read, briefly,
and then forgets it.

So the fix is **not** to start archiving messages server-side. That would undo
the product's central guarantee. The fix is to make the client's local store —
which is *designed* to be the durable copy — actually durable.

## What already exists

The persistent implementation is already written and shipping on mobile:

- `MessageStore implements MessageRepo` (`client-core/src/db/messageStore.ts`)
  is a full SQL-backed store with the schema in `db/schema.ts` and its own tests.
- It talks to a tiny `SqliteDB` port — `exec(sql)`, `run(sql, params)`,
  `all(sql, params)` — and nothing else.
- Mobile satisfies that port with `expo-sqlite` (`mobile/src/platform/expoSqlite.ts`).

**Web is the only client with no `SqliteDB` implementation**, which is why it
falls back to memory. The gap is one adapter, not a persistence layer.

## Plan

- [x] **Phase 1 — A `SqliteDB` for the browser.** `clients/web/src/platform/opfsSqlite.ts`
  implements the port over SQLite-wasm with the OPFS SAHPool VFS (durable, and
  unlike the SharedArrayBuffer path it needs no COOP/COEP headers), falling back
  to an in-memory SQLite database when OPFS is unavailable (private windows,
  older browsers) so the app degrades to the old behaviour instead of failing to
  start.
- [x] **Phase 2 — Use it.** The worker builds a `MessageStore` over that database
  and `init()` creates the schema and hydrates cursors from disk. Cursor
  hydration is what makes a reload *resume* rather than re-fetch.
- [x] **Phase 3 — Verify.** Done live against the dev stack: sent two messages,
  hard-reloaded twice, and both the chat list and the thread came back. The
  fallback path was exercised repeatedly while debugging Phase 2 and behaves as
  designed (app boots, warns, runs on memory).

## Two bugs found on the way

**`MessageStore` was missing `markReceipt`.** The worker calls it on every
receipt, but only `MemoryMessageRepo` had it — it was not even on the
`MessageRepo` interface, so the compiler could not see the hole. Added to the
interface and implemented in SQL with a monotonic rank guard, so an out-of-order
`DELIVERED` arriving after a `READ` cannot walk a bubble's ticks backwards.
`markSent` got the same guard for the same reason.

**`AppServices.create()` was not a singleton.** It constructed a new
`AppServices` — and therefore a new DB worker — on every call, and React
StrictMode invokes the provider effect twice in development. The second worker
was discarded but never terminated. With `MemoryMessageRepo` that was invisible
(two maps, no contention); with SQLite on OPFS the leaked worker holds the
storage lock **forever**, so the surviving worker silently fell back to a
non-persistent database and the fix appeared not to work. `create()` now memoises
its boot promise.

## Known limitation: one tab at a time

The OPFS SAHPool VFS is exclusive — a second tab of the app cannot open the same
database. It retries briefly (a reload legitimately races the outgoing page's
worker) and then falls back to memory, and `StorageWarning` tells the user that
tab will not save anything rather than letting them type into a store that is
about to be discarded. The real fix is a `SharedWorker` so every tab talks to one
database; the RPC boundary in `worker/rpc.ts` is already the right seam for it.

## Note on what persistence does and does not restore

Messages the client has already received are restored from local disk. Messages
that arrived while the client was gone are replayed from the server inbox using
the hydrated cursors — that is what the cursors are for. Messages a device
already ACKed and then lost *without* a local copy are gone, by design: nobody
kept them. Persisting locally is therefore not just a convenience here, it is
the only thing that makes history exist at all.
