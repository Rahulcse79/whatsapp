// SqliteOutboxStore is the persistent implementation of @wa/sync-engine's
// OutboxStore port over the SqliteDB seam — this is the "sqlite local db wired
// to @wa/sync-engine" the mobile shell requires. The crash-safety semantics
// (persist-before-send, remove-only-on-ack) live in @wa/sync-engine; this class
// only translates rows.

import type { OutboxEntry, OutboxStore } from "@wa/sync-engine";
import type { SqliteDB, SqlRow } from "../ports";

const SELECT_ALL =
  "SELECT client_ref, conversation_id, payload, attempts, created_at FROM outbox ORDER BY created_at ASC, client_ref ASC";

export class SqliteOutboxStore implements OutboxStore {
  constructor(private readonly db: SqliteDB) {}

  async enqueue(e: OutboxEntry): Promise<void> {
    await this.db.run(
      "INSERT OR REPLACE INTO outbox(client_ref, conversation_id, payload, attempts, created_at) VALUES(?,?,?,?,?)",
      [e.clientRef, e.conversationId, e.payload, e.attempts, e.createdAt],
    );
  }

  async list(): Promise<OutboxEntry[]> {
    const rows = await this.db.all(SELECT_ALL);
    return rows.map(toEntry);
  }

  async remove(clientRef: string): Promise<void> {
    await this.db.run("DELETE FROM outbox WHERE client_ref = ?", [clientRef]);
  }

  async update(e: OutboxEntry): Promise<void> {
    await this.db.run("UPDATE outbox SET attempts = ? WHERE client_ref = ?", [e.attempts, e.clientRef]);
  }
}

function toEntry(r: SqlRow): OutboxEntry {
  return {
    clientRef: String(r.client_ref),
    conversationId: String(r.conversation_id),
    payload: r.payload instanceof Uint8Array ? r.payload : new Uint8Array(),
    attempts: Number(r.attempts),
    createdAt: Number(r.created_at),
  };
}
