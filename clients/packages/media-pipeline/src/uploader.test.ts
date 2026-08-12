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
});
