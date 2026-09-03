import { createRequire } from "node:module";
import { Cursors } from "@wa/sync-engine";
import { beforeEach, describe, expect, it } from "vitest";
import type { SqliteDB, SqlRow, SqlValue } from "../ports";
import { MsgKind, type InboxBatch } from "../frames";
import { MessageStore } from "./messageStore";

// Loaded through createRequire because Vite's dependency scanner does not know
// the (newer) node:sqlite builtin and tries to resolve it from disk.
const { DatabaseSync } = createRequire(import.meta.url)("node:sqlite") as typeof import("node:sqlite");
type Db = InstanceType<typeof DatabaseSync>;

// A SqliteDB over a real SQLite engine, so these exercise the store's actual SQL
// (including the CASE-based monotonic guards) rather than a hand-written fake
// that could quietly disagree with it. node:sqlite is synchronous; the port is
// async, which is exactly the shape of the browser and expo-sqlite adapters.
function nodeSqlite(db: Db): SqliteDB {
  return {
    async exec(sql) {
      db.exec(sql);
    },
    async run(sql, params: SqlValue[] = []) {
      db.prepare(sql).run(...(params as never[]));
    },
    async all<T extends SqlRow = SqlRow>(sql: string, params: SqlValue[] = []) {
      return db.prepare(sql).all(...(params as never[])) as T[];
    },
  };
}

const inbound = (seq: number, body: string): InboxBatch => ({
  t: "inbox_batch",
  items: [
    {
      conversationId: "c1",
      seq,
      msgUuid: `in-${seq}`,
      senderUserId: "u2",
      senderDeviceId: "d2",
      kind: MsgKind.TEXT,
      ciphertext: new Uint8Array([seq]),
      acceptedAtMs: 9_000 + seq,
    },
  ],
});

describe("MessageStore on a real SQLite database", () => {
  let db: Db;
  let store: MessageStore;

  beforeEach(async () => {
    db = new DatabaseSync(":memory:");
    store = new MessageStore(nodeSqlite(db), new Cursors());
    await store.init();
  });

  it("survives being reopened: a second store over the same file sees the history", async () => {
    await store.enqueueOutgoing({
      clientRef: "out-1",
      conversationId: "c1",
      plaintext: "hello from before the reload",
      payload: new Uint8Array([1, 2, 3]),
      now: 5_000,
    });
    await store.markSent("out-1", 7);
    await store.persistInboxBatch(inbound(8, "and a reply"), new Map([["in-8", "and a reply"]]));

    // A fresh store over the same database is exactly what a page reload does.
    const reopened = new MessageStore(nodeSqlite(db), new Cursors());
    const cursors = await reopened.init();

    expect(await reopened.conversations()).toHaveLength(1);
    expect((await reopened.thread("c1")).map((m) => m.body)).toEqual(["hello from before the reload", "and a reply"]);
    // Cursors hydrate too, so the reconnect resumes instead of re-fetching.
    expect(cursors).toEqual([{ conversationId: "c1", lastSeq: 8 }]);
  });

  it("markReceipt advances my own sent messages to delivered, then read", async () => {
    await store.enqueueOutgoing({ clientRef: "m1", conversationId: "c1", plaintext: "one", payload: new Uint8Array(), now: 1 });
    await store.markSent("m1", 1);

    await store.markReceipt("c1", "DELIVERED", 1);
    expect((await store.thread("c1"))[0]?.state).toBe("delivered");

    await store.markReceipt("c1", "READ", 1);
    expect((await store.thread("c1"))[0]?.state).toBe("read");
  });

  it("never walks ticks backwards when a receipt arrives out of order", async () => {
    await store.enqueueOutgoing({ clientRef: "m1", conversationId: "c1", plaintext: "one", payload: new Uint8Array(), now: 1 });
    await store.markSent("m1", 1);
    await store.markReceipt("c1", "READ", 1);

    // A DELIVERED that overtook its READ must not downgrade the bubble.
    await store.markReceipt("c1", "DELIVERED", 1);
    expect((await store.thread("c1"))[0]?.state).toBe("read");
  });

  it("leaves the peer's messages and unacked sends alone", async () => {
    await store.persistInboxBatch(inbound(3, "theirs"), new Map([["in-3", "theirs"]]));
    await store.enqueueOutgoing({ clientRef: "m2", conversationId: "c1", plaintext: "still sending", payload: new Uint8Array(), now: 2 });

    await store.markReceipt("c1", "READ", 99);

    const byBody = new Map((await store.thread("c1")).map((m) => [m.body, m.state]));
    expect(byBody.get("theirs")).toBe("received"); // inbound never gets ticks
    expect(byBody.get("still sending")).toBe("sending"); // seq 0 — not yet acked
  });

  it("does not let a late ack undo a receipt that raced ahead of it", async () => {
    await store.enqueueOutgoing({ clientRef: "m1", conversationId: "c1", plaintext: "one", payload: new Uint8Array(), now: 1 });
    await store.markSent("m1", 4);
    await store.markReceipt("c1", "READ", 4);

    await store.markSent("m1", 4); // a duplicate ack
    expect((await store.thread("c1"))[0]?.state).toBe("read");
  });
});
