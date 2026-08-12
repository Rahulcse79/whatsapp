import { describe, expect, it } from "vitest";
import { toBase64 } from "./base64";
import type { DownloadTransport } from "./download";
import { DownloadManager, type DownloadItem, type DownloadState } from "./downloadManager";
import type { MediaEnvelope } from "./envelope";
import { encryptMedia } from "./mediaCrypto";

/** Build a real envelope (+ its ciphertext) so fetchAndDecrypt's hash check and
 *  AES-GCM decrypt actually pass — the manager is exercised against real crypto,
 *  not a stub. */
async function makeMedia(objectKey: string, plaintext: Uint8Array): Promise<{ env: MediaEnvelope; ciphertext: Uint8Array; plaintext: Uint8Array }> {
  const { key, ciphertext, contentHash } = await encryptMedia(plaintext);
  const env: MediaEnvelope = {
    objectKey,
    fileKey: toBase64(key),
    contentHash: toBase64(contentHash),
    sizeBytes: ciphertext.length,
    mime: "image/webp",
  };
  return { env, ciphertext, plaintext };
}

/** A transport whose GETs park until the test releases them — lets us observe
 *  how many downloads run at once and drive success/failure deterministically. */
class GatedTransport implements DownloadTransport {
  readonly calls: string[] = [];
  concurrentPeak = 0;
  private inflight = 0;
  private readonly gates = new Map<string, { resolve: (b: Uint8Array) => void; reject: (e: Error) => void }>();

  get(objectKey: string): Promise<Uint8Array> {
    this.calls.push(objectKey);
    this.inflight++;
    this.concurrentPeak = Math.max(this.concurrentPeak, this.inflight);
    return new Promise<Uint8Array>((resolve, reject) => {
      this.gates.set(objectKey, {
        resolve: (b) => {
          this.inflight--;
          resolve(b);
        },
        reject: (e) => {
          this.inflight--;
          reject(e);
        },
      });
    });
  }

  release(objectKey: string, bytes: Uint8Array): void {
    this.gates.get(objectKey)?.resolve(bytes);
    this.gates.delete(objectKey);
  }
  fail(objectKey: string, err: Error): void {
    this.gates.get(objectKey)?.reject(err);
    this.gates.delete(objectKey);
  }
}

function waitFor(m: DownloadManager, key: string, state: DownloadState): Promise<DownloadItem> {
  return new Promise<DownloadItem>((resolve, reject) => {
    const cur = m.get(key);
    if (cur?.state === state) {
      resolve(cur);
      return;
    }
    let unsub = (): void => {};
    const timer = setTimeout(() => {
      unsub();
      reject(new Error(`timeout waiting for ${key} → ${state}`));
    }, 3000);
    unsub = m.subscribe((item) => {
      if (item.objectKey === key && item.state === state) {
        clearTimeout(timer);
        unsub();
        resolve(item);
      }
    });
  });
}

describe("DownloadManager", () => {
  it("downloads, decrypts, and caches — a second request re-uses the bytes", async () => {
    const t = new GatedTransport();
    const m = new DownloadManager({ transport: t });
    const { env, ciphertext, plaintext } = await makeMedia("obj-1", Uint8Array.from([1, 2, 3, 4, 5]));

    const first = m.request(env);
    expect(["queued", "downloading"]).toContain(first.state); // started (concurrency slot free)

    t.release("obj-1", ciphertext);
    const ready = await waitFor(m, "obj-1", "ready");
    expect(ready.bytes).toEqual(plaintext);

    // Cache hit: same object key, no new GET.
    const again = m.request(env);
    expect(again.state).toBe("ready");
    expect(again.bytes).toEqual(plaintext);
    expect(t.calls).toEqual(["obj-1"]); // exactly one network fetch
  });

  it("never runs more than `concurrency` downloads at once", async () => {
    const t = new GatedTransport();
    const m = new DownloadManager({ transport: t, concurrency: 2 });
    const media = await Promise.all([0, 1, 2, 3].map((i) => makeMedia(`obj-${i}`, Uint8Array.from([i]))));

    for (const { env } of media) m.request(env);

    // Only the first two started; the rest wait behind the concurrency gate.
    expect(t.calls).toEqual(["obj-0", "obj-1"]);

    // Finishing one admits exactly one more.
    t.release("obj-0", media[0]!.ciphertext);
    await waitFor(m, "obj-0", "ready");
    expect(t.calls).toContain("obj-2");
    expect(t.calls).not.toContain("obj-3");

    t.release("obj-1", media[1]!.ciphertext);
    await waitFor(m, "obj-1", "ready");
    expect(t.calls).toContain("obj-3");

    t.release("obj-2", media[2]!.ciphertext);
    t.release("obj-3", media[3]!.ciphertext);
    await Promise.all([waitFor(m, "obj-2", "ready"), waitFor(m, "obj-3", "ready")]);

    expect(t.concurrentPeak).toBeLessThanOrEqual(2);
  });

  it("surfaces an error, then recovers on retry", async () => {
    const t = new GatedTransport();
    const m = new DownloadManager({ transport: t });
    const { env, ciphertext, plaintext } = await makeMedia("obj-x", Uint8Array.from([9, 9, 9]));

    m.request(env);
    t.fail("obj-x", new Error("network down"));
    const errored = await waitFor(m, "obj-x", "error");
    expect(errored.error).toMatch(/network down/);
    expect(errored.attempts).toBe(1);

    m.retry("obj-x");
    t.release("obj-x", ciphertext);
    const recovered = await waitFor(m, "obj-x", "ready");
    expect(recovered.bytes).toEqual(plaintext);
    expect(recovered.attempts).toBe(2);
    expect(t.calls).toEqual(["obj-x", "obj-x"]); // one failed + one successful attempt
  });

  it("rejects (as an error item) when the stored bytes fail the hash check", async () => {
    const t = new GatedTransport();
    const m = new DownloadManager({ transport: t });
    const { env, ciphertext } = await makeMedia("obj-bad", Uint8Array.from([4, 5, 6]));

    m.request(env);
    const corrupt = Uint8Array.from(ciphertext);
    corrupt[corrupt.length - 1] = (corrupt[corrupt.length - 1]! ^ 0xff) & 0xff;
    t.release("obj-bad", corrupt);

    const errored = await waitFor(m, "obj-bad", "error");
    expect(errored.error).toMatch(/content hash mismatch/);
  });
});
