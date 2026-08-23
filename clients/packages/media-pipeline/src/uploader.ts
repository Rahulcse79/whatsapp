// Resumable multipart upload (HLD §9, steps 3–5; sequence-diagrams §4). The
// orchestration is deliberately transport-agnostic: media-svc control-plane
// calls and the direct-to-object-storage part PUTs both go through an injected
// {@link UploadTransport}, so this logic is unit-testable in Node and reusable
// across web (fetch) and React Native without pulling a DOM/networking dep in.
//
// Flow:  create → PUT every part → (any failed?) re-presign just those parts and
// retry → complete. The server verifies size + content hash on complete; a byte
// that changed in transit fails there, not silently. Resumption is the whole
// point — a dropped connection re-presigns only the missing parts, never the
// bytes already stored.

import type { MediaEnvelope } from "./envelope";

/** One presigned part slot returned by media-svc (`PartURL`). */
export interface PartURL {
  part_number: number;
  url: string;
}

/** A stored part's identity, sent back on complete (`PartETag`). */
export interface PartETag {
  part_number: number;
  etag: string;
}

/** Response of `POST /v1/media/uploads` (`CreateResult`). */
export interface CreateUploadResult {
  upload_id: string;
  object_key: string;
  part_urls: PartURL[];
  part_size: number;
}

/** The transport seam. `postJSON` speaks to media-svc (bearer-authenticated,
 *  JSON in/out); `putPart` streams one ciphertext slice straight to object
 *  storage via its presigned URL and returns the stored ETag. */
export interface UploadTransport {
  postJSON<T>(path: string, body: unknown): Promise<T>;
  putPart(url: string, bytes: Uint8Array): Promise<string>;
}

/** What the caller gets once every part is durable and the upload is finalized. */
export interface UploadOutcome {
  mediaId: string;
  objectKey: string;
  sizeBytes: number;
}

const CREATE_PATH = "/v1/media/uploads";

/** ResumableUploader drives one ciphertext blob to completion over the transport. */
export class ResumableUploader {
  private readonly partConcurrency: number;

  constructor(
    private readonly transport: UploadTransport,
    /** How many times to re-presign-and-retry the still-missing parts. */
    private readonly maxResumes = 3,
    /** How many part PUTs may be in flight at once (T15.04 upload tuning).
     *  Parts were previously uploaded strictly one at a time, which left a
     *  multi-part upload bound by per-request latency rather than by
     *  bandwidth. A small window keeps a mobile radio busy without starving
     *  the rest of the app or tripping per-connection limits. */
    partConcurrency = 4,
  ) {
    this.partConcurrency = Math.max(1, partConcurrency);
  }

  /** upload stores `ciphertext` and returns the finalized media identity.
   *  `contentHashB64` and `size` must describe exactly these bytes — media-svc
   *  re-derives and rejects a mismatch on complete. */
  async upload(ciphertext: Uint8Array, contentHashB64: string, mime: string): Promise<UploadOutcome> {
    const created = await this.transport.postJSON<CreateUploadResult>(CREATE_PATH, {
      size: ciphertext.length,
      content_hash: contentHashB64,
      mime_claimed: mime,
    });

    const etags = new Map<number, string>();
    let pending: PartURL[] = created.part_urls;

    for (let attempt = 0; ; attempt++) {
      const missing = await this.putParts(pending, ciphertext, created.part_size, etags);
      if (missing.length === 0) break;
      if (attempt >= this.maxResumes) {
        throw new Error(`media upload failed: ${missing.length} part(s) unrecoverable after ${this.maxResumes} resume(s)`);
      }
      // Resumption primitive: ask media-svc for fresh URLs for ONLY the parts
      // that are still missing, then loop to retry exactly those.
      const res = await this.transport.postJSON<{ part_urls: PartURL[] }>(
        `${CREATE_PATH}/${encodeURIComponent(created.upload_id)}/presign`,
        { missing_parts: missing },
      );
      pending = res.part_urls;
    }

    const partsEtags: PartETag[] = [...etags.entries()]
      .sort((a, b) => a[0] - b[0])
      .map(([part_number, etag]) => ({ part_number, etag }));

    const done = await this.transport.postJSON<{ media_id: string }>(
      `${CREATE_PATH}/${encodeURIComponent(created.upload_id)}/complete`,
      { parts_etags: partsEtags },
    );

    return { mediaId: done.media_id, objectKey: created.object_key, sizeBytes: ciphertext.length };
  }

  /** putParts PUTs each not-yet-stored part and records its ETag; it returns the
   *  part numbers that failed (to be re-presigned by the caller). Already-stored
   *  parts are skipped, which is what makes a retry cheap. */
  private async putParts(
    parts: PartURL[],
    ciphertext: Uint8Array,
    partSize: number,
    etags: Map<number, string>,
  ): Promise<number[]> {
    // Only the parts still outstanding; an already-stored part is never re-PUT,
    // which is what makes a resume cheap.
    const todo = parts.filter((p) => !etags.has(p.part_number));
    const missing: number[] = [];

    // A fixed pool of workers drains the queue, so at most `partConcurrency`
    // PUTs are in flight regardless of how many parts the blob has. Failures
    // are collected rather than thrown: the caller re-presigns exactly the
    // missing parts and loops.
    let next = 0;
    const worker = async (): Promise<void> => {
      for (;;) {
        const i = next++;
        const part = todo[i];
        if (!part) return;
        const start = (part.part_number - 1) * partSize;
        const slice = ciphertext.subarray(start, Math.min(start + partSize, ciphertext.length));
        try {
          const etag = await this.transport.putPart(part.url, slice);
          etags.set(part.part_number, etag);
        } catch {
          missing.push(part.part_number);
        }
      }
    };
    await Promise.all(Array.from({ length: Math.min(this.partConcurrency, todo.length) }, worker));

    // Ascending order keeps retries and logs deterministic even though the
    // parts completed out of order.
    return missing.sort((a, b) => a - b);
  }
}

/** partCount is how many `partSize` slices `total` bytes splits into (>=1). */
export function partCount(total: number, partSize: number): number {
  if (partSize <= 0) throw new Error("partSize must be positive");
  return Math.max(1, Math.ceil(total / partSize));
}

// Re-exported so callers can build an envelope from an upload outcome without a
// second import; the envelope is the pipeline's real output (see pipeline.ts).
export type { MediaEnvelope };
