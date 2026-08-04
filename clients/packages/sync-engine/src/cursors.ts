// Per-conversation replay cursors drive delta sync: the client tracks the
// highest seq it has persisted per conversation and detects gaps
// (Docs/11-clients/offline-sync-local-store.md §3). The ACK-after-persist rule
// means the cursor only advances once a message is durably stored.

/** Cursors tracks the last persisted seq per conversation. */
export class Cursors {
  private readonly lastSeq = new Map<string, number>();

  /** get returns the last persisted seq for a conversation (0 if none). */
  get(conversationId: string): number {
    return this.lastSeq.get(conversationId) ?? 0;
  }

  /**
   * advance records that a conversation is persisted up to seq, but only ever
   * forwards — an out-of-order or replayed lower seq never rewinds the cursor.
   * Returns true if the cursor moved.
   */
  advance(conversationId: string, seq: number): boolean {
    if (seq > this.get(conversationId)) {
      this.lastSeq.set(conversationId, seq);
      return true;
    }
    return false;
  }

  /**
   * gapBefore reports whether receiving `seq` implies a gap — i.e. seq is more
   * than one past the cursor, so the client must SyncPull the missing range.
   * A seq at or below the cursor is a duplicate (no gap); exactly cursor+1 is
   * contiguous.
   */
  gapBefore(conversationId: string, seq: number): boolean {
    return seq > this.get(conversationId) + 1;
  }

  /** snapshot exports the cursors for a resume Hello frame. */
  snapshot(): Array<{ conversationId: string; lastSeq: number }> {
    return [...this.lastSeq.entries()].map(([conversationId, lastSeq]) => ({
      conversationId,
      lastSeq,
    }));
  }

  /** load restores cursors persisted locally. */
  load(entries: Array<{ conversationId: string; lastSeq: number }>): void {
    for (const { conversationId, lastSeq } of entries) {
      this.lastSeq.set(conversationId, lastSeq);
    }
  }
}
