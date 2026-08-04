// The outbox is the client's crash-safe send queue. Design invariant
// (Docs/11-clients/offline-sync-local-store.md §2): an entry is durably
// persisted before a send is attempted and removed ONLY after the server
// acknowledges (or permanently rejects) it. A crash mid-send leaves the entry
// in place; the resend carries the same client_ref (UUIDv7) and the server
// dedupes it — so the pipeline never loses a message and never delivers a
// duplicate, however the network or app fails.

/** MessageState mirrors the local DB state column. */
export type MessageState =
  | "sending"
  | "sent"
  | "delivered"
  | "read"
  | "failed"
  | "received";

/** OutboxEntry is one pending send. clientRef is the UUIDv7 idempotency key. */
export interface OutboxEntry {
  clientRef: string;
  conversationId: string;
  /** Sealed envelope — opaque bytes; the outbox never inspects content. */
  payload: Uint8Array;
  attempts: number;
  createdAt: number;
}

/** The server's verdict on a send. */
export type SendResult =
  | { status: "acked"; seq: number }
  | { status: "retry" } // transient — keep and resend
  | { status: "permanent" }; // rejected for good (e.g. window closed) — drop

/** SendFn ships one entry. It may throw (network/crash) — treated as retry. */
export type SendFn = (entry: OutboxEntry) => Promise<SendResult>;

/**
 * OutboxStore is the persistence port (SQLCipher on device). list() returns
 * entries oldest-first so sends preserve authoring order.
 */
export interface OutboxStore {
  enqueue(entry: OutboxEntry): Promise<void>;
  list(): Promise<OutboxEntry[]>;
  remove(clientRef: string): Promise<void>;
  update(entry: OutboxEntry): Promise<void>;
}

/** Result of one flush pass. */
export interface FlushStats {
  acked: number;
  retried: number;
  failed: number;
}

/** MemoryOutboxStore is an in-process OutboxStore for tests and dev. */
export class MemoryOutboxStore implements OutboxStore {
  private readonly entries = new Map<string, OutboxEntry>();

  async enqueue(entry: OutboxEntry): Promise<void> {
    this.entries.set(entry.clientRef, { ...entry });
  }
  async list(): Promise<OutboxEntry[]> {
    return [...this.entries.values()].sort(
      (a, b) => a.createdAt - b.createdAt || a.clientRef.localeCompare(b.clientRef),
    );
  }
  async remove(clientRef: string): Promise<void> {
    this.entries.delete(clientRef);
  }
  async update(entry: OutboxEntry): Promise<void> {
    this.entries.set(entry.clientRef, { ...entry });
  }
  /** size is a test convenience. */
  get size(): number {
    return this.entries.size;
  }
}

/** Outbox drives the crash-safe flush loop over an OutboxStore. */
export class Outbox {
  constructor(
    private readonly store: OutboxStore,
    private readonly send: SendFn,
  ) {}

  /** enqueue persists the entry before any send is attempted. */
  async enqueue(entry: OutboxEntry): Promise<void> {
    await this.store.enqueue(entry);
  }

  /**
   * flush attempts every pending entry once. An entry is removed only on an
   * ack (delivered) or a permanent rejection; a transient result or a thrown
   * error keeps it for the next pass (no loss). The send carries the stable
   * clientRef, so a resend after a lost ack is deduped server-side (no dupe).
   */
  async flush(): Promise<FlushStats> {
    const stats: FlushStats = { acked: 0, retried: 0, failed: 0 };
    for (const entry of await this.store.list()) {
      let result: SendResult;
      try {
        result = await this.send(entry);
      } catch {
        // Crash / network failure — the server may or may not have received
        // it. Keep the entry; the resend is idempotent.
        await this.store.update({ ...entry, attempts: entry.attempts + 1 });
        stats.retried++;
        continue;
      }
      switch (result.status) {
        case "acked":
          await this.store.remove(entry.clientRef);
          stats.acked++;
          break;
        case "retry":
          await this.store.update({ ...entry, attempts: entry.attempts + 1 });
          stats.retried++;
          break;
        case "permanent":
          await this.store.remove(entry.clientRef);
          stats.failed++;
          break;
      }
    }
    return stats;
  }

  /** drain flushes repeatedly until the outbox empties or maxRounds elapse. */
  async drain(maxRounds = 1000): Promise<FlushStats> {
    const total: FlushStats = { acked: 0, retried: 0, failed: 0 };
    for (let round = 0; round < maxRounds; round++) {
      if ((await this.store.list()).length === 0) break;
      const s = await this.flush();
      total.acked += s.acked;
      total.retried += s.retried;
      total.failed += s.failed;
    }
    return total;
  }

  /** pending returns the current queue depth. */
  async pending(): Promise<number> {
    return (await this.store.list()).length;
  }
}
