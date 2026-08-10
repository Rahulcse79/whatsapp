import { Outbox, type OutboxEntry } from "@wa/sync-engine";
import { describe, expect, it } from "vitest";
import type { SqliteDB, SqlRow, SqlValue } from "../ports";
import { SqliteOutboxStore } from "./outboxStore";

// A tiny SqliteDB fake that understands only the outbox statements
// SqliteOutboxStore issues (the SQL strings are constants we control).
interface Row {
  client_ref: string;
  conversation_id: string;
  payload: Uint8Array;
  attempts: number;
  created_at: number;
}
class FakeSqliteDB implements SqliteDB {
  readonly outbox = new Map<string, Row>();
  exec(): Promise<void> {
    return Promise.resolve();
  }
  run(sql: string, params: SqlValue[] = []): Promise<void> {
    if (sql.startsWith("INSERT OR REPLACE INTO outbox")) {
      const [cr, conv, payload, attempts, created] = params;
      this.outbox.set(String(cr), {
        client_ref: String(cr),
        conversation_id: String(conv),
        payload: payload instanceof Uint8Array ? payload : new Uint8Array(),
        attempts: Number(attempts),
        created_at: Number(created),
      });
    } else if (sql.startsWith("DELETE FROM outbox")) {
      this.outbox.delete(String(params[0]));
    } else if (sql.startsWith("UPDATE outbox SET attempts")) {
      const row = this.outbox.get(String(params[1]));
      if (row) row.attempts = Number(params[0]);
    }
    return Promise.resolve();
  }
  all<T extends SqlRow = SqlRow>(sql: string): Promise<T[]> {
    if (sql.includes("FROM outbox")) {
      const rows = [...this.outbox.values()].sort(
        (a, b) => a.created_at - b.created_at || a.client_ref.localeCompare(b.client_ref),
      );
      return Promise.resolve(rows as unknown as T[]);
    }
    return Promise.resolve([]);
  }
}

const entry = (clientRef: string, createdAt: number, attempts = 0): OutboxEntry => ({
  clientRef,
  conversationId: "c1",
  payload: new Uint8Array([createdAt]),
  attempts,
  createdAt,
});

describe("SqliteOutboxStore", () => {
  it("persists, lists oldest-first, updates attempts, and removes", async () => {
    const store = new SqliteOutboxStore(new FakeSqliteDB());
    await store.enqueue(entry("b", 2));
    await store.enqueue(entry("a", 1));

    let list = await store.list();
    expect(list.map((e) => e.clientRef)).toEqual(["a", "b"]); // by created_at

    await store.update(entry("a", 1, 3));
    list = await store.list();
    expect(list.find((e) => e.clientRef === "a")?.attempts).toBe(3);

    await store.remove("a");
    expect((await store.list()).map((e) => e.clientRef)).toEqual(["b"]);
  });

  it("drives @wa/sync-engine's Outbox over SQLite (persist → ack → drain)", async () => {
    const store = new SqliteOutboxStore(new FakeSqliteDB());
    const ob = new Outbox(store, () => Promise.resolve({ status: "acked", seq: 1 }));
    await ob.enqueue(entry("m1", 1));
    await ob.enqueue(entry("m2", 2));
    expect(await ob.pending()).toBe(2);

    await ob.drain();
    expect(await ob.pending()).toBe(0); // acked entries cleared from the SQLite store
  });
});
