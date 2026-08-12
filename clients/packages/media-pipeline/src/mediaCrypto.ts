// Per-file media encryption (HLD §9, step 2). Every attachment gets a fresh,
// random 256-bit key; the plaintext is sealed in independent AES-256-GCM chunks
// so a client can stream-encrypt (and later stream-decrypt) a 25 MB file without
// holding the whole thing decrypted in memory, and so a corrupted byte range
// fails only its own chunk's tag rather than the entire blob.
//
// Unlike the crypto-wrapper's session ciphers (which are INSECURE dev doubles
// for the message ratchet), THIS is real cryptography: WebCrypto's AES-GCM.
// libsignal owns the *session* layer; bulk media is symmetric AEAD under a key
// that itself travels inside the Signal-encrypted envelope, so rolling it on the
// platform's vetted AES-GCM is correct, not a hand-rolled primitive.
//
// Wire format of the ciphertext blob (what gets uploaded to object storage):
//
//     repeated chunk := u32BE(ctLen) ‖ iv(12) ‖ ct(ctLen)
//
// where ct is GCM output (ciphertext ‖ 16-byte tag) for one <= CHUNK_SIZE slice
// of plaintext. A fresh random IV per chunk keeps (key, IV) pairs unique.

/** Plaintext chunk size. 256 KiB balances per-chunk overhead against memory. */
export const CHUNK_SIZE = 256 * 1024;

const IV_BYTES = 12; // 96-bit nonce — the GCM standard/native size
const KEY_BYTES = 32; // AES-256
const LEN_PREFIX = 4; // u32BE ciphertext length per chunk

/** The product of encrypting one file: the key stays client-side (it rides the
 *  E2EE envelope), the ciphertext is uploaded, and the hash is what media-svc
 *  verifies on completion. */
export interface EncryptedMedia {
  /** 32-byte AES-256 key — placed (base64) into the recipient's envelope. */
  key: Uint8Array;
  /** Framed AES-GCM ciphertext blob, uploaded verbatim to object storage. */
  ciphertext: Uint8Array;
  /** SHA-256 of `ciphertext` — the integrity check media-svc enforces. */
  contentHash: Uint8Array;
}

/** encryptMedia seals `plaintext` under a fresh random file key and returns the
 *  upload blob plus its content hash. */
export async function encryptMedia(plaintext: Uint8Array): Promise<EncryptedMedia> {
  const rawKey = randomBytes(KEY_BYTES);
  const ciphertext = await encryptWithKey(rawKey, plaintext);
  const contentHash = await sha256(ciphertext);
  return { key: rawKey, ciphertext, contentHash };
}

/** encryptWithKey seals `plaintext` under a caller-supplied key. Used for the
 *  encrypted mini-thumbnail, which reuses the file key so a single envelope key
 *  unlocks both the full media and its preview. */
export async function encryptWithKey(rawKey: Uint8Array, plaintext: Uint8Array): Promise<Uint8Array> {
  const key = await importKey(rawKey, "encrypt");
  const frames: Uint8Array[] = [];
  for (let off = 0; off < plaintext.length; off += CHUNK_SIZE) {
    const chunk = plaintext.subarray(off, Math.min(off + CHUNK_SIZE, plaintext.length));
    const iv = randomBytes(IV_BYTES);
    const sealed = new Uint8Array(await crypto.subtle.encrypt({ name: "AES-GCM", iv }, key, chunk));
    frames.push(frame(iv, sealed));
  }
  return concat(frames);
}

/** decryptMedia reverses {@link encryptWithKey}: it walks the framed blob,
 *  authenticating and decrypting each chunk. A wrong key or any tampered byte
 *  makes AES-GCM reject that chunk, rejecting the returned promise. */
export async function decryptMedia(rawKey: Uint8Array, ciphertext: Uint8Array): Promise<Uint8Array> {
  const key = await importKey(rawKey, "decrypt");
  const plainChunks: Uint8Array[] = [];
  let off = 0;
  while (off < ciphertext.length) {
    if (off + LEN_PREFIX + IV_BYTES > ciphertext.length) {
      throw new Error("media ciphertext truncated: incomplete chunk header");
    }
    const ctLen = readU32(ciphertext, off);
    off += LEN_PREFIX;
    const iv = ciphertext.subarray(off, off + IV_BYTES);
    off += IV_BYTES;
    if (off + ctLen > ciphertext.length) {
      throw new Error("media ciphertext truncated: chunk body shorter than its length prefix");
    }
    const ct = ciphertext.subarray(off, off + ctLen);
    off += ctLen;
    const plain = new Uint8Array(await crypto.subtle.decrypt({ name: "AES-GCM", iv }, key, ct));
    plainChunks.push(plain);
  }
  return concat(plainChunks);
}

/** sha256 returns the SHA-256 digest of `data` (used for the content hash). */
export async function sha256(data: Uint8Array): Promise<Uint8Array> {
  return new Uint8Array(await crypto.subtle.digest("SHA-256", data));
}

// ── internals ────────────────────────────────────────────────────────────────

function importKey(rawKey: Uint8Array, usage: "encrypt" | "decrypt"): Promise<CryptoKey> {
  if (rawKey.length !== KEY_BYTES) {
    return Promise.reject(new Error(`media key must be ${KEY_BYTES} bytes, got ${rawKey.length}`));
  }
  return crypto.subtle.importKey("raw", rawKey, "AES-GCM", false, [usage]);
}

function frame(iv: Uint8Array, ct: Uint8Array): Uint8Array {
  const out = new Uint8Array(LEN_PREFIX + iv.length + ct.length);
  writeU32(out, 0, ct.length);
  out.set(iv, LEN_PREFIX);
  out.set(ct, LEN_PREFIX + iv.length);
  return out;
}

function randomBytes(n: number): Uint8Array {
  return crypto.getRandomValues(new Uint8Array(n));
}

function concat(parts: Uint8Array[]): Uint8Array {
  let len = 0;
  for (const p of parts) len += p.length;
  const out = new Uint8Array(len);
  let off = 0;
  for (const p of parts) {
    out.set(p, off);
    off += p.length;
  }
  return out;
}

function writeU32(buf: Uint8Array, off: number, n: number): void {
  buf[off] = (n >>> 24) & 0xff;
  buf[off + 1] = (n >>> 16) & 0xff;
  buf[off + 2] = (n >>> 8) & 0xff;
  buf[off + 3] = n & 0xff;
}

function readU32(buf: Uint8Array, off: number): number {
  return ((buf[off]! << 24) | (buf[off + 1]! << 16) | (buf[off + 2]! << 8) | buf[off + 3]!) >>> 0;
}
