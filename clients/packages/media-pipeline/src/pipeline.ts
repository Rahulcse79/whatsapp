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

import { toBase64 } from "./base64";
import { decryptThumbnail, fetchAndDecrypt, type DownloadTransport } from "./download";
import type { MediaEnvelope } from "./envelope";
import { encryptMedia, encryptWithKey } from "./mediaCrypto";
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

export type { DownloadTransport };

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
   *  against the envelope, then decrypt. Delegates to {@link fetchAndDecrypt} so
   *  the download path is usable without constructing an uploader. */
  open(envelope: MediaEnvelope, transport: DownloadTransport): Promise<Uint8Array> {
    return fetchAndDecrypt(envelope, transport);
  }

  /** openThumbnail decrypts just the inline preview, if the envelope carries one. */
  openThumbnail(envelope: MediaEnvelope): Promise<Uint8Array | null> {
    return decryptThumbnail(envelope);
  }
}
