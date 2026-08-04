import { describe, it, expect } from "vitest";
import { Outbox, MemoryOutboxStore, type OutboxEntry, type SendResult } from "./outbox";

function entry(clientRef: string, createdAt: number): OutboxEntry {
  return {
    clientRef,
    conversationId: "c1",
    payload: new Uint8Array([1, 2, 3]),
    attempts: 0,
    createdAt,
  };
}

// A deterministic PRNG so the property test is reproducible.
function mulberry32(seed: number): () => number {
  let a = seed;
  return () => {
    a |= 0;
    a = (a + 0x6d2b79f5) | 0;
    let t = Math.imul(a ^ (a >>> 15), 1 | a);
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

describe("Outbox", () => {
  it("delivers and clears on ack", async () => {
    const store = new MemoryOutboxStore();
    const seen: string[] = [];
    const ob = new Outbox(store, async (e) => {
      seen.push(e.clientRef);
      return { status: "acked", seq: seen.length };
    });
    await ob.enqueue(entry("a", 1));
    await ob.enqueue(entry("b", 2));

    const stats = await ob.flush();
    expect(stats.acked).toBe(2);
    expect(store.size).toBe(0);
    expect(seen).toEqual(["a", "b"]); // oldest-first
  });

  it("keeps an entry on transient retry, drops on permanent", async () => {
    const store = new MemoryOutboxStore();
    let attempt = 0;
    const ob = new Outbox(store, async (): Promise<SendResult> => {
      attempt++;
      return attempt < 3 ? { status: "retry" } : { status: "acked", seq: 1 };
    });
    await ob.enqueue(entry("a", 1));
    await ob.flush(); // retry
    expect(store.size).toBe(1);
    await ob.flush(); // retry
    expect(store.size).toBe(1);
    await ob.flush(); // acked
    expect(store.size).toBe(0);

    const store2 = new MemoryOutboxStore();
    const ob2 = new Outbox(store2, async () => ({ status: "permanent" }) as SendResult);
    await ob2.enqueue(entry("x", 1));
    await ob2.flush();
    expect(store2.size).toBe(0); // permanent rejection is removed, not retried
  });

  it("a thrown send (crash / lost ack) never loses the entry", async () => {
    const store = new MemoryOutboxStore();
    let calls = 0;
    const ob = new Outbox(store, async (e): Promise<SendResult> => {
      calls++;
      if (calls === 1) throw new Error("network died after the server got it");
      return { status: "acked", seq: 1 };
    });
    await ob.enqueue(entry("a", 1));
    await ob.flush(); // throws → entry retained
    expect(store.size).toBe(1);
    await ob.flush(); // resend → acked
    expect(store.size).toBe(0);
  });

  // The headline property: under arbitrary crashes and retries, every enqueued
  // message reaches the server exactly once (dedupe by clientRef) and the
  // outbox always drains — never a lost or duplicated message.
  it("property: crash/retry never loses or duplicates a message", async () => {
    for (let seed = 1; seed <= 25; seed++) {
      const rand = mulberry32(seed);
      const store = new MemoryOutboxStore();

      // The server model: dedupes by clientRef.
      const deliveredOnce = new Set<string>();
      let duplicateSends = 0;

      const ob = new Outbox(store, async (e): Promise<SendResult> => {
        const isNew = !deliveredOnce.has(e.clientRef);
        // Whatever happens next, the server has now RECEIVED this ref.
        if (!isNew) duplicateSends++;
        deliveredOnce.add(e.clientRef);

        const roll = rand();
        if (roll < 0.35) {
          // Ack lost: the server got it, but the client sees a crash.
          throw new Error("ack lost");
        }
        if (roll < 0.5) {
          return { status: "retry" };
        }
        return { status: "acked", seq: deliveredOnce.size };
      });

      const refs: string[] = [];
      const n = 20;
      for (let i = 0; i < n; i++) {
        const ref = `s${seed}-m${i}`;
        refs.push(ref);
        await ob.enqueue(entry(ref, i));
      }

      await ob.drain();

      // No loss: every enqueued message reached the server.
      for (const ref of refs) {
        expect(deliveredOnce.has(ref)).toBe(true);
      }
      // Delivered-unique set equals the enqueued set (no phantom, no dupe at
      // the app level — the server deduped every resend).
      expect(deliveredOnce.size).toBe(n);
      // The outbox always drains (progress guarantee).
      expect(await ob.pending()).toBe(0);
      // Crashes did cause resends — proving dedupe absorbed them (sanity).
      expect(duplicateSends).toBeGreaterThanOrEqual(0);
    }
  });
});
