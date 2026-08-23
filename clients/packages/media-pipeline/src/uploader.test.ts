import { describe, expect, it } from "vitest";
import {
  partCount,
  ResumableUploader,
  type CreateUploadResult,
  type PartETag,
  type PartURL,
  type UploadTransport,
} from "./uploader";

/** In-memory stand-in for media-svc + object storage. Parts are addressed by a
 *  `put://part/<n>` URL; `failUntil` makes a part's PUT throw for its first N
 *  attempts, modeling a dropped connection that resumption must recover. */
class FakeTransport implements UploadTransport {
  readonly stored = new Map<number, Uint8Array>();
  readonly presignCalls: number[][] = [];
  completeBody: PartETag[] | null = null;
  private readonly attempts = new Map<number, number>();
  readonly numParts: number;

  constructor(
    readonly ciphertextLen: number,
    readonly partSize: number,
    private readonly failUntil: Map<number, number> = new Map(),
    private readonly createOrder?: number[],
  ) {
    this.numParts = Math.max(1, Math.ceil(ciphertextLen / partSize));
  }

  postJSON<T>(path: string, body: unknown): Promise<T> {
    if (path === "/v1/media/uploads") {
      const order = this.createOrder ?? range(1, this.numParts);
      const res: CreateUploadResult = {
        upload_id: "up_1",
        object_key: "media/2026/ab/cd/blob",
        part_urls: order.map(partURL),
        part_size: this.partSize,
      };
      return Promise.resolve(res as T);
    }
    if (path.endsWith("/presign")) {
      const missing = (body as { missing_parts: number[] }).missing_parts;
      this.presignCalls.push([...missing]);
      return Promise.resolve({ part_urls: missing.map(partURL) } as T);
    }
    if (path.endsWith("/complete")) {
      this.completeBody = (body as { parts_etags: PartETag[] }).parts_etags;
      return Promise.resolve({ media_id: "med_1" } as T);
    }
    return Promise.reject(new Error(`unexpected path ${path}`));
  }

  putPart(url: string, bytes: Uint8Array): Promise<string> {
    const n = Number(url.slice(url.lastIndexOf("/") + 1));
    const attempt = (this.attempts.get(n) ?? 0) + 1;
    this.attempts.set(n, attempt);
    if (attempt <= (this.failUntil.get(n) ?? 0)) {
      return Promise.reject(new Error(`simulated failure: part ${n} attempt ${attempt}`));
    }
    this.stored.set(n, Uint8Array.from(bytes)); // copy: uploader passes a subarray view
    return Promise.resolve(`etag-${n}`);
  }

  /** Reassemble stored parts in part order — must equal the original blob. */
  reassemble(): Uint8Array {
    const out = new Uint8Array(this.ciphertextLen);
    let off = 0;
    for (let n = 1; n <= this.numParts; n++) {
      const part = this.stored.get(n);
      if (!part) throw new Error(`part ${n} missing`);
      out.set(part, off);
      off += part.length;
    }
    return out;
  }
}

function partURL(n: number): PartURL {
  return { part_number: n, url: `put://part/${n}` };
}
function range(lo: number, hi: number): number[] {
  const out: number[] = [];
  for (let n = lo; n <= hi; n++) out.push(n);
  return out;
}
function blob(len: number): Uint8Array {
  const out = new Uint8Array(len);
  for (let i = 0; i < len; i++) out[i] = (i * 31 + 3) & 0xff;
  return out;
}

describe("partCount", () => {
  it("ceilings to whole parts, minimum one", () => {
    expect(partCount(0, 100)).toBe(1);
    expect(partCount(1, 100)).toBe(1);
    expect(partCount(100, 100)).toBe(1);
    expect(partCount(101, 100)).toBe(2);
    expect(partCount(250, 100)).toBe(3);
  });
});

describe("ResumableUploader", () => {
  it("uploads every part, completes with sorted etags, and returns the media id", async () => {
    const data = blob(250);
    const t = new FakeTransport(250, 100); // 3 parts: 100/100/50
    const outcome = await new ResumableUploader(t).upload(data, "aGFzaA==", "image/webp");

    expect(outcome).toEqual({ mediaId: "med_1", objectKey: "media/2026/ab/cd/blob", sizeBytes: 250 });
    expect(t.reassemble()).toEqual(data); // bytes stored intact and in the right slots
    expect(t.completeBody?.map((p) => p.part_number)).toEqual([1, 2, 3]);
    expect(t.completeBody).toEqual([
      { part_number: 1, etag: "etag-1" },
      { part_number: 2, etag: "etag-2" },
      { part_number: 3, etag: "etag-3" },
    ]);
    expect(t.presignCalls).toEqual([]); // nothing failed → no resume
  });

  it("re-presigns and retries ONLY the parts that failed", async () => {
    const data = blob(250);
    const t = new FakeTransport(250, 100, new Map([[2, 1]])); // part 2 fails once
    const outcome = await new ResumableUploader(t).upload(data, "aGFzaA==", "video/mp4");

    expect(outcome.mediaId).toBe("med_1");
    expect(t.presignCalls).toEqual([[2]]); // resumed exactly the missing part
    expect(t.reassemble()).toEqual(data); // recovered blob is intact
    expect(t.completeBody?.map((p) => p.part_number)).toEqual([1, 2, 3]);
  });

  it("gives up after maxResumes when a part never recovers", async () => {
    const t = new FakeTransport(250, 100, new Map([[2, 99]])); // part 2 always fails
    const uploader = new ResumableUploader(t, 2);
    await expect(uploader.upload(blob(250), "aGFzaA==", "image/webp")).rejects.toThrow(/unrecoverable after 2 resume/);
    expect(t.presignCalls).toEqual([[2], [2]]); // resumed twice, then gave up
    expect(t.completeBody).toBeNull(); // never finalized
  });

  it("sorts etags by part number even when the server hands parts out of order", async () => {
    const data = blob(250);
    const t = new FakeTransport(250, 100, new Map(), [3, 1, 2]); // create lists parts shuffled
    await new ResumableUploader(t).upload(data, "aGFzaA==", "application/octet-stream");
    expect(t.completeBody?.map((p) => p.part_number)).toEqual([1, 2, 3]);
    expect(t.reassemble()).toEqual(data);
  });

  // ── Protocol scenario P13 (test-strategy.md §3): upload-interrupt-resume-
  //    complete; hash mismatch → resumability, reject path. ──────────────────

  it("P13: recovers from a mid-upload interruption, re-presigning ONLY the interrupted window", async () => {
    const partSize = 100;
    const data = blob(20 * partSize); // 20 parts
    // A dropped connection fails parts 7..12 on their first attempt.
    const failWindow = new Map<number, number>();
    for (let n = 7; n <= 12; n++) failWindow.set(n, 1);
    const t = new FakeTransport(20 * partSize, partSize, failWindow);

    const outcome = await new ResumableUploader(t).upload(data, "aGFzaA==", "application/octet-stream");

    expect(outcome.mediaId).toBe("med_1");
    expect(t.presignCalls).toEqual([[7, 8, 9, 10, 11, 12]]); // resumed exactly the missing parts
    expect(t.reassemble()).toEqual(data); // every byte recovered, in the right slots
    expect(t.completeBody?.map((p) => p.part_number)).toEqual(range(1, 20)); // all finalized, sorted
  });

  it("P13: surfaces the server's reject on a content-hash mismatch (never reports success)", async () => {
    const base = new FakeTransport(250, 100);
    // media-svc re-derives the hash on complete and rejects a mismatch; the
    // uploader must propagate that failure rather than fabricate a media id.
    const rejectingComplete: UploadTransport = {
      postJSON<T>(path: string, body: unknown): Promise<T> {
        if (path.endsWith("/complete")) return Promise.reject(new Error("MEDIA_HASH_MISMATCH"));
        return base.postJSON<T>(path, body);
      },
      putPart: (url, bytes) => base.putPart(url, bytes),
    };
    await expect(new ResumableUploader(rejectingComplete).upload(blob(250), "wronghash", "image/webp")).rejects.toThrow(
      /HASH_MISMATCH/,
    );
  });

  it("P13: a 25 MB blob splits into independently-resumable parts (GATE P1 scale)", () => {
    const MB = 1024 * 1024;
    expect(partCount(25 * MB, 5 * MB)).toBe(5); // 5 MB parts → five resumable slices
    expect(partCount(25 * MB + 1, 5 * MB)).toBe(6); // a trailing byte spills into one more part
  });
});

// ── Part-upload concurrency (T15.04) ────────────────────────────────────────
// Parts used to be PUT strictly one at a time, so a multi-part upload was bound
// by per-request latency rather than bandwidth. These cover the window without
// weakening any of the resume guarantees above.

/** Records how many PUTs are in flight simultaneously so the cap is observable. */
class ConcurrencyProbeTransport implements UploadTransport {
  maxInFlight = 0;
  private inFlight = 0;
  readonly stored = new Map<number, Uint8Array>();
  readonly numParts: number;

  constructor(
    readonly ciphertextLen: number,
    readonly partSize: number,
    private readonly delayMs = 5,
  ) {
    this.numParts = Math.max(1, Math.ceil(ciphertextLen / partSize));
  }

  postJSON<T>(path: string, _body: unknown): Promise<T> {
    if (path.endsWith("/complete")) return Promise.resolve({ media_id: "m1" } as T);
    if (path.endsWith("/presign")) return Promise.resolve({ part_urls: [] } as T);
    const part_urls: PartURL[] = [];
    for (let n = 1; n <= this.numParts; n++) part_urls.push({ part_number: n, url: `put://part/${n}` });
    return Promise.resolve({ upload_id: "u1", object_key: "k1", part_urls, part_size: this.partSize } as T);
  }

  async putPart(url: string, bytes: Uint8Array): Promise<string> {
    this.inFlight++;
    this.maxInFlight = Math.max(this.maxInFlight, this.inFlight);
    try {
      await new Promise((r) => setTimeout(r, this.delayMs));
      const n = Number(url.split("/").pop());
      this.stored.set(n, bytes);
      return `etag-${n}`;
    } finally {
      this.inFlight--;
    }
  }
}

describe("part upload concurrency", () => {
  it("uploads parts in parallel up to the configured window", async () => {
    const t = new ConcurrencyProbeTransport(1000, 100); // 10 parts
    await new ResumableUploader(t, 3, 4).upload(new Uint8Array(1000), "h", "application/octet-stream");
    expect(t.stored.size).toBe(10);
    expect(t.maxInFlight).toBeGreaterThan(1); // actually parallel
    expect(t.maxInFlight).toBeLessThanOrEqual(4); // and capped
  });

  it("never exceeds a window of 1 (serial) when asked", async () => {
    const t = new ConcurrencyProbeTransport(500, 100); // 5 parts
    await new ResumableUploader(t, 3, 1).upload(new Uint8Array(500), "h", "application/octet-stream");
    expect(t.maxInFlight).toBe(1);
    expect(t.stored.size).toBe(5);
  });

  it("still stores every part when the window is wider than the part count", async () => {
    const t = new ConcurrencyProbeTransport(150, 100); // 2 parts, window 8
    await new ResumableUploader(t, 3, 8).upload(new Uint8Array(150), "h", "application/octet-stream");
    expect(t.stored.size).toBe(2);
    expect(t.maxInFlight).toBeLessThanOrEqual(2);
  });

  it("reports missing parts in ascending order despite out-of-order completion", async () => {
    // Parts 2 and 4 fail forever; the uploader must surface them sorted so
    // retries and logs stay deterministic.
    const fails = new Map([
      [2, Number.MAX_SAFE_INTEGER],
      [4, Number.MAX_SAFE_INTEGER],
    ]);
    const t = new FakeTransport(500, 100, fails); // 5 parts
    await expect(
      new ResumableUploader(t, 1, 4).upload(new Uint8Array(500), "h", "application/octet-stream"),
    ).rejects.toThrow(/unrecoverable/);
    // Each presign round asks for exactly the still-missing parts, in order.
    for (const call of t.presignCalls) expect(call).toEqual([...call].sort((a, b) => a - b));
  });
});
