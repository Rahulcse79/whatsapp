import { describe, expect, it } from "vitest";
import { fromBase64, toBase64 } from "./base64";
import { sha256 } from "./mediaCrypto";
import {
  MediaPipeline,
  type Compressor,
  type DownloadTransport,
  type MediaSource,
  type Preview,
  type ThumbnailMaker,
} from "./pipeline";
import { ResumableUploader, type CreateUploadResult, type PartETag, type PartURL, type UploadTransport } from "./uploader";

/** A fake that is BOTH the upload target and the download source: parts PUT
 *  during `prepare` are reassembled on complete and served back by `get`, so a
 *  full send→receive round-trip can be exercised with no network. */
class FakeObjectStore implements UploadTransport, DownloadTransport {
  private readonly parts = new Map<number, Uint8Array>();
  readonly finalized = new Map<string, Uint8Array>();
  private readonly partSize = 100;
  private readonly objectKey = "media/2026/aa/bb/blob";
  private numParts = 0;
  private totalLen = 0;

  postJSON<T>(path: string, body: unknown): Promise<T> {
    if (path === "/v1/media/uploads") {
      this.totalLen = (body as { size: number }).size;
      this.numParts = Math.max(1, Math.ceil(this.totalLen / this.partSize));
      const part_urls: PartURL[] = [];
      for (let n = 1; n <= this.numParts; n++) part_urls.push({ part_number: n, url: `put://part/${n}` });
      const res: CreateUploadResult = { upload_id: "u1", object_key: this.objectKey, part_urls, part_size: this.partSize };
      return Promise.resolve(res as T);
    }
    if (path.endsWith("/complete")) {
      void (body as { parts_etags: PartETag[] });
      const blob = new Uint8Array(this.totalLen);
      let off = 0;
      for (let n = 1; n <= this.numParts; n++) {
        const part = this.parts.get(n);
        if (!part) throw new Error(`part ${n} missing on complete`);
        blob.set(part, off);
        off += part.length;
      }
      this.finalized.set(this.objectKey, blob);
      return Promise.resolve({ media_id: "m1" } as T);
    }
    return Promise.reject(new Error(`unexpected path ${path}`));
  }

  putPart(url: string, bytes: Uint8Array): Promise<string> {
    const n = Number(url.slice(url.lastIndexOf("/") + 1));
    this.parts.set(n, Uint8Array.from(bytes));
    return Promise.resolve(`etag-${n}`);
  }

  get(objectKey: string): Promise<Uint8Array> {
    const blob = this.finalized.get(objectKey);
    if (!blob) return Promise.reject(new Error(`no object ${objectKey}`));
    return Promise.resolve(blob);
  }
}

function source(len: number, mime = "image/png"): MediaSource {
  const bytes = new Uint8Array(len);
  for (let i = 0; i < len; i++) bytes[i] = (i * 13 + 5) & 0xff;
  return { bytes, mime };
}

describe("MediaPipeline", () => {
  it("prepares an envelope and opens it back to the original bytes", async () => {
    const store = new FakeObjectStore();
    const pipeline = new MediaPipeline(new ResumableUploader(store));
    const src = { ...source(1000, "image/png"), width: 640, height: 480 };

    const env = await pipeline.prepare(src);

    // Envelope carries what the recipient needs and the metadata passthrough.
    expect(env.objectKey).toBe("media/2026/aa/bb/blob");
    expect(env.mime).toBe("image/png");
    expect(env.width).toBe(640);
    expect(env.height).toBe(480);
    expect(fromBase64(env.fileKey).length).toBe(32);
    expect(env.encThumb).toBeUndefined(); // no thumbnailer configured
    expect(env.blurhash).toBeUndefined();

    // contentHash / sizeBytes describe exactly the stored ciphertext blob.
    const stored = store.finalized.get(env.objectKey)!;
    expect(env.sizeBytes).toBe(stored.length);
    expect(env.contentHash).toBe(toBase64(await sha256(stored)));

    // Recipient side recovers the plaintext.
    const recovered = await pipeline.open(env, store);
    expect(recovered).toEqual(src.bytes);
  });

  it("runs the compressor and uploads the compressed representation", async () => {
    const compressor: Compressor = {
      compress: (s) => Promise.resolve({ bytes: s.bytes.subarray(0, 200), mime: "image/webp", width: 64, height: 64 }),
    };
    const store = new FakeObjectStore();
    const pipeline = new MediaPipeline(new ResumableUploader(store), { compressor });

    const env = await pipeline.prepare(source(1000, "image/png"));

    expect(env.mime).toBe("image/webp"); // compressed mime, not the source mime
    expect(env.width).toBe(64);
    const recovered = await pipeline.open(env, store);
    expect(recovered).toEqual(source(1000).bytes.subarray(0, 200)); // the compressed bytes
  });

  it("attaches an encrypted mini-thumbnail unlocked by the file key", async () => {
    const thumbBytes = Uint8Array.from([9, 8, 7, 6, 5, 4, 3, 2, 1, 0]);
    const thumbnailer: ThumbnailMaker = {
      make: (): Promise<Preview | null> => Promise.resolve({ thumbnail: thumbBytes, blurhash: "LKO2?U%2Tw=w" }),
    };
    const store = new FakeObjectStore();
    const pipeline = new MediaPipeline(new ResumableUploader(store), { thumbnailer });

    const env = await pipeline.prepare(source(500));

    expect(env.blurhash).toBe("LKO2?U%2Tw=w");
    expect(env.encThumb).toBeDefined();
    // The same envelope key decrypts the inline preview.
    const thumb = await pipeline.openThumbnail(env);
    expect(thumb).toEqual(thumbBytes);
  });

  it("refuses to decrypt when the stored bytes no longer match the envelope hash", async () => {
    const store = new FakeObjectStore();
    const pipeline = new MediaPipeline(new ResumableUploader(store));
    const env = await pipeline.prepare(source(300));

    // Corrupt the object at rest after upload.
    const stored = store.finalized.get(env.objectKey)!;
    stored[0] = (stored[0]! ^ 0xff) & 0xff;

    await expect(pipeline.open(env, store)).rejects.toThrow(/content hash mismatch/);
  });

  it("openThumbnail returns null when there is no preview", async () => {
    const store = new FakeObjectStore();
    const pipeline = new MediaPipeline(new ResumableUploader(store));
    const env = await pipeline.prepare(source(300));
    expect(await pipeline.openThumbnail(env)).toBeNull();
  });
});
