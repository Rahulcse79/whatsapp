// The recipient side of the media path (HLD §9, step 7), factored out of the
// send pipeline so a download needs no uploader: fetch the stored ciphertext,
// re-verify its content hash against the envelope, then decrypt. A hash mismatch
// means the bytes at rest are not what the sender sealed — we refuse to decrypt.

import { fromBase64, toBase64 } from "./base64";
import type { MediaEnvelope } from "./envelope";
import { decryptMedia, sha256 } from "./mediaCrypto";

/** DownloadTransport fetches a stored ciphertext blob by its object key. The
 *  concrete impl resolves a presigned GET (media-svc /download-urls) and streams
 *  the bytes; injected so the recipient path is testable without a network. */
export interface DownloadTransport {
  get(objectKey: string): Promise<Uint8Array>;
}

/** fetchAndDecrypt downloads, verifies, and decrypts the full attachment. */
export async function fetchAndDecrypt(envelope: MediaEnvelope, transport: DownloadTransport): Promise<Uint8Array> {
  const ciphertext = await transport.get(envelope.objectKey);
  const actualHash = toBase64(await sha256(ciphertext));
  if (actualHash !== envelope.contentHash) {
    throw new Error("media content hash mismatch: stored bytes differ from the envelope");
  }
  return decryptMedia(fromBase64(envelope.fileKey), ciphertext);
}

/** decryptThumbnail unlocks the inline mini-thumbnail (sealed under the same
 *  file key), or null when the envelope carries no preview. No network. */
export async function decryptThumbnail(envelope: MediaEnvelope): Promise<Uint8Array | null> {
  if (!envelope.encThumb) return null;
  return decryptMedia(fromBase64(envelope.fileKey), fromBase64(envelope.encThumb));
}
