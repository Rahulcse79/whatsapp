import { describe, expect, it } from "vitest";
import {
  BootstrapReceiver,
  BootstrapSender,
  frameRecords,
  unframeRecords,
  type ChunkCipher,
  type HistoryChunk,
} from "./bootstrap";

/** A reversible non-crypto cipher standing in for the session cipher: XOR with a
 *  constant so the engine's seal/open plumbing is exercised (production injects
 *  crypto-wrapper's real SessionCipher). */
const xorCipher: ChunkCipher = {
  seal: (p) => Promise.resolve(p.map((b) => b ^ 0x5a)),
  open: (s) => Promise.resolve(s.map((b) => b ^ 0x5a)),
};

const rec = (...vals: number[]) => Uint8Array.from(vals);
function records(n: number): Uint8Array[] {
  return Array.from({ length: n }, (_, i) => rec(i & 0xff, (i * 7 + 1) & 0xff, (i * 13) & 0xff));
}

/** Drives a full transfer, delivering only the chunk seqs in `deliver` (others
 *  are "dropped"); returns the receiver for assertions. */
async function run(all: Uint8Array[], chunkRecords: number, deliver?: (seq: number) => boolean) {
  const sender = new BootstrapSender("t1", all, xorCipher, chunkRecords);
  const imported: Uint8Array[] = [];
  const receiver = new BootstrapReceiver(xorCipher, (rs) => {
    imported.push(...rs);
  });
  receiver.setManifest(sender.manifest());
  for (let seq = 0; seq < sender.totalChunks(); seq++) {
    if (deliver && !deliver(seq)) continue;
    await receiver.accept(await sender.chunk(seq));
  }
  return { sender, receiver, imported };
}

describe("frameRecords / unframeRecords", () => {
  it("round-trips a batch, including empty records and an empty batch", () => {
    const batch = [rec(1, 2, 3), new Uint8Array(0), rec(9)];
    expect(unframeRecords(frameRecords(batch))).toEqual(batch);
    expect(unframeRecords(frameRecords([]))).toEqual([]);
  });

  it("throws on a truncated frame", () => {
    const framed = frameRecords([rec(1, 2, 3, 4)]);
    expect(() => unframeRecords(framed.subarray(0, framed.length - 2))).toThrow(/truncated/);
  });
});

describe("BootstrapSender manifest", () => {
  it("counts chunks by ceiling of records / chunkRecords", () => {
    expect(new BootstrapSender("t", records(0), xorCipher, 10).manifest()).toMatchObject({ totalChunks: 0, totalRecords: 0 });
    expect(new BootstrapSender("t", records(10), xorCipher, 10).totalChunks()).toBe(1);
    expect(new BootstrapSender("t", records(21), xorCipher, 10).totalChunks()).toBe(3);
  });

  it("rejects an out-of-range chunk", async () => {
    const s = new BootstrapSender("t", records(5), xorCipher, 10);
    await expect(s.chunk(1)).rejects.toThrow(/out of range/);
  });
});

describe("history bootstrap transfer", () => {
  it("transfers every record and completes (E2E ciphertext, decrypted on receipt)", async () => {
    const all = records(45);
    const { receiver, imported } = await run(all, 10); // 5 chunks
    expect(receiver.complete()).toBe(true);
    expect(receiver.progress()).toEqual({ received: 5, total: 5 });
    expect(imported).toEqual(all);
  });

  it("relays only ciphertext — the sealed chunk is not the plaintext", async () => {
    const s = new BootstrapSender("t", records(3), xorCipher, 10);
    const chunk = await s.chunk(0);
    const framed = frameRecords(records(3));
    expect(chunk.sealed).not.toEqual(framed); // sealed differs from the framed plaintext
    expect(await xorCipher.open(chunk.sealed)).toEqual(framed); // but opens back to it
  });

  it("is resumable: only the missing chunks are re-sent, no double-import", async () => {
    const all = records(45); // 5 chunks of 10 (last is 5)
    // First pass drops chunks 1 and 3.
    const sender = new BootstrapSender("t", all, xorCipher, 10);
    const imported: Uint8Array[] = [];
    const receiver = new BootstrapReceiver(xorCipher, (rs) => {
      imported.push(...rs);
    });
    receiver.setManifest(sender.manifest());
    for (const seq of [0, 2, 4]) await receiver.accept(await sender.chunk(seq));

    expect(receiver.complete()).toBe(false);
    expect(receiver.missing()).toEqual([1, 3]);

    // Resume: re-send the missing (and redundantly re-send 0 to prove dedup).
    for (const seq of [0, 1, 3]) await receiver.accept(await sender.chunk(seq));

    expect(receiver.complete()).toBe(true);
    expect(imported).toHaveLength(45); // every record exactly once (no double-import)
  });

  it("restore() resumes progress after a restart", async () => {
    const sender = new BootstrapSender("t", records(45), xorCipher, 10);
    const receiver = new BootstrapReceiver(xorCipher, () => {});
    receiver.setManifest(sender.manifest());
    receiver.restore([0, 1, 2]); // persisted from before the restart
    expect(receiver.missing()).toEqual([3, 4]);
    expect(receiver.progress()).toEqual({ received: 3, total: 5 });
  });

  it("an empty history completes immediately", async () => {
    const { receiver } = await run(records(0), 10);
    expect(receiver.complete()).toBe(true);
    expect(receiver.missing()).toEqual([]);
  });
});
