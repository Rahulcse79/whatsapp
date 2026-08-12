// End-to-end client media pipeline (HLD §9). Ties the pieces together:
//
//   send:  source ─▶ [compress] ─▶ [thumbnail+blurhash] ─▶ encrypt ─▶ upload
//                                                              └▶ MediaEnvelope
//   recv:  MediaEnvelope ─▶ download ─▶ verify hash ─▶ decrypt ─▶ plaintext
//
// Compression and thumbnail generation are PORTS, not code here: they depend on
// platform codecs (browser Canvas / WebCodecs, React Native native modules) that
// can't run — or be meaningfully tested — in this framework-free package. The
// crypto, framing, hashing, and upload orchestration are all real and tested.

import { fromBase64, toBase64 } from "./base64";
import type { MediaEnvelope } from "./envelope";
import { decryptMedia, encryptMedia, encryptWithKey, sha256 } from "./mediaCrypto";
import type { ResumableUploader } from "./uploader";

/** Raw source bytes plus the metadata the pipeline threads into the envelope. */
export interface MediaSource {
  bytes: Uint8Array;
  mime: string;
  width?: number;
  height?: number;
  durationMs?: number;
}

/** A preview: a tiny thumbnail (encrypted under the file key) + a BlurHash. */
export interface Preview {
  thumbnail: Uint8Array;
  blurhash: string;
}

/** Compressor is the platform codec port (WebP/AVIF/H.264/Opus, HLD §9 step 1).
 *  Returning the input unchanged is a valid no-op for already-optimal media. */
export interface Compressor {
  compress(source: MediaSource): Promise<MediaSource>;
}

/** ThumbnailMaker produces the inline preview (HLD §9 step 1). Returns null when
 *  the media type has no visual preview (e.g. a voice note has only a waveform). */
export interface ThumbnailMaker {
  make(source: MediaSource): Promise<Preview | null>;
}

/** DownloadTransport fetches a stored ciphertext blob by its object key. The
 *  concrete impl resolves a presigned GET (media-svc /download-urls) and streams
 *  the bytes; injected so the recipient path is testable without a network. */
export interface DownloadTransport {
  get(objectKey: string): Promise<Uint8Array>;
}

/** MediaPipeline prepares outbound media and reconstructs inbound media. The
 *  compressor and thumbnailer are optional — omit them and the pipeline uploads
 *  the source bytes as-is with no preview. */
export class MediaPipeline {
  constructor(
    private readonly uploader: ResumableUploader,
    private readonly opts: { compressor?: Compressor; thumbnailer?: ThumbnailMaker } = {},
  ) {}

  /** prepare compresses, previews, encrypts, and uploads `source`, returning the
   *  E2EE envelope to seal inside the outgoing message. */
  async prepare(source: MediaSource): Promise<MediaEnvelope> {
    const compressed = this.opts.compressor ? await this.opts.compressor.compress(source) : source;
    const preview = this.opts.thumbnailer ? await this.opts.thumbnailer.make(compressed) : null;

    const { key, ciphertext, contentHash } = await encryptMedia(compressed.bytes);
    const contentHashB64 = toBase64(contentHash);

    const outcome = await this.uploader.upload(ciphertext, contentHashB64, compressed.mime);

    const envelope: MediaEnvelope = {
      objectKey: outcome.objectKey,
      fileKey: toBase64(key),
      contentHash: contentHashB64,
      sizeBytes: outcome.sizeBytes,
      mime: compressed.mime,
    };
    if (compressed.width !== undefined) envelope.width = compressed.width;
    if (compressed.height !== undefined) envelope.height = compressed.height;
    if (compressed.durationMs !== undefined) envelope.durationMs = compressed.durationMs;
    if (preview) {
      // The mini-thumbnail reuses the file key: one envelope secret unlocks both
      // the full media and its preview.
      envelope.blurhash = preview.blurhash;
      envelope.encThumb = toBase64(await encryptWithKey(key, preview.thumbnail));
    }
    return envelope;
  }

  /** open is the recipient side: download the blob, re-verify its content hash
   *  against the envelope, then decrypt. A hash mismatch means the stored bytes
   *  are not what the sender sealed — we refuse to decrypt them. */
  async open(envelope: MediaEnvelope, transport: DownloadTransport): Promise<Uint8Array> {
    const ciphertext = await transport.get(envelope.objectKey);
    const actualHash = toBase64(await sha256(ciphertext));
    if (actualHash !== envelope.contentHash) {
      throw new Error("media content hash mismatch: stored bytes differ from the envelope");
    }
    return decryptMedia(fromBase64(envelope.fileKey), ciphertext);
  }

  /** openThumbnail decrypts just the inline preview, if the envelope carries one. */
  async openThumbnail(envelope: MediaEnvelope): Promise<Uint8Array | null> {
    if (!envelope.encThumb) return null;
    return decryptMedia(fromBase64(envelope.fileKey), fromBase64(envelope.encThumb));
  }
}
