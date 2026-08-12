// The media envelope (HLD §9, step 6). This is the small JSON object the sender
// places INSIDE the Signal-encrypted message. It never travels in clear: the
// file key that unlocks the attachment rides here, protected end-to-end by the
// session ratchet. The server sees only opaque object storage and this envelope's
// ciphertext — never the key, never the plaintext, never the true content type.

/** MediaEnvelope is the per-attachment descriptor carried in the E2EE payload. */
export interface MediaEnvelope {
  /** Object-storage key of the uploaded ciphertext blob (from media-svc). */
  objectKey: string;
  /** base64 of the 32-byte AES-256 file key — the secret this envelope guards. */
  fileKey: string;
  /** base64 SHA-256 of the ciphertext; the recipient re-checks it before decrypt. */
  contentHash: string;
  /** Byte length of the uploaded ciphertext blob. */
  sizeBytes: number;
  /** MIME type the sender claims (advisory; recipients still sniff/verify). */
  mime: string;
  /** BlurHash placeholder rendered instantly while the full media downloads. */
  blurhash?: string;
  /** base64 of the encrypted mini-thumbnail (sealed under {@link fileKey}). */
  encThumb?: string;
  /** Original width in pixels, when the source was an image or video. */
  width?: number;
  /** Original height in pixels, when the source was an image or video. */
  height?: number;
  /** Duration in milliseconds, for audio/video. */
  durationMs?: number;
}
