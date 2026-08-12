import { describe, expect, it } from "vitest";
import { CHUNK_SIZE, decryptMedia, encryptMedia, encryptWithKey, sha256 } from "./mediaCrypto";

// These exercise REAL AES-256-GCM via WebCrypto (Node's globalThis.crypto),
// unlike the crypto-wrapper dev doubles. No mocking of the primitive.

function bytes(...vals: number[]): Uint8Array {
  return Uint8Array.from(vals);
}

/** A deterministic, recognizable plaintext so we can assert it never surfaces
 *  in the ciphertext. */
function pattern(len: number): Uint8Array {
  const out = new Uint8Array(len);
  for (let i = 0; i < len; i++) out[i] = (i * 7 + 0x11) & 0xff;
  return out;
}

/** contiguous-subsequence search — used to prove no plaintext run leaks. */
function contains(haystack: Uint8Array, needle: Uint8Array): boolean {
  outer: for (let i = 0; i + needle.length <= haystack.length; i++) {
    for (let j = 0; j < needle.length; j++) {
      if (haystack[i + j] !== needle[j]) continue outer;
    }
    return true;
  }
  return false;
}

describe("encryptMedia / decryptMedia", () => {
  it("round-trips a small single-chunk payload", async () => {
    const plaintext = bytes(1, 2, 3, 4, 5, 6, 7, 8, 9, 10);
    const { key, ciphertext } = await encryptMedia(plaintext);
    const recovered = await decryptMedia(key, ciphertext);
    expect(recovered).toEqual(plaintext);
  });

  it("round-trips a multi-chunk payload larger than CHUNK_SIZE", async () => {
    const plaintext = pattern(CHUNK_SIZE * 2 + 777); // spans 3 chunks
    const { key, ciphertext } = await encryptMedia(plaintext);
    const recovered = await decryptMedia(key, ciphertext);
    expect(recovered.length).toBe(plaintext.length);
    expect(recovered).toEqual(plaintext);
  });

  it("uses a fresh 32-byte key each call and never emits the plaintext", async () => {
    const marker = pattern(4096);
    const a = await encryptMedia(marker);
    const b = await encryptMedia(marker);
    expect(a.key.length).toBe(32);
    expect(a.key).not.toEqual(b.key); // random per file
    expect(a.ciphertext).not.toEqual(b.ciphertext); // random IVs
    expect(contains(a.ciphertext, marker)).toBe(false); // no plaintext run
  });

  it("content hash is SHA-256 of exactly the ciphertext bytes", async () => {
    const { ciphertext, contentHash } = await encryptMedia(pattern(5000));
    expect(contentHash).toEqual(await sha256(ciphertext));
    expect(contentHash.length).toBe(32);
  });

  it("rejects decryption under the wrong key (GCM tag fails)", async () => {
    const { ciphertext } = await encryptMedia(pattern(1000));
    const wrongKey = crypto.getRandomValues(new Uint8Array(32));
    await expect(decryptMedia(wrongKey, ciphertext)).rejects.toBeTruthy();
  });

  it("rejects a tampered ciphertext byte", async () => {
    const { key, ciphertext } = await encryptMedia(pattern(1000));
    const last = ciphertext.length - 1;
    ciphertext[last] = (ciphertext[last]! ^ 0x01) & 0xff; // flip a bit in the last chunk's tag/body
    await expect(decryptMedia(key, ciphertext)).rejects.toBeTruthy();
  });

  it("rejects a truncated frame header", async () => {
    const { key, ciphertext } = await encryptMedia(pattern(1000));
    await expect(decryptMedia(key, ciphertext.subarray(0, 6))).rejects.toThrow(/truncated/);
  });

  it("rejects a key of the wrong length", async () => {
    await expect(encryptWithKey(new Uint8Array(16), pattern(10))).rejects.toThrow(/32 bytes/);
  });

  it("handles an empty payload", async () => {
    const { key, ciphertext } = await encryptMedia(new Uint8Array(0));
    expect(ciphertext.length).toBe(0);
    expect(await decryptMedia(key, ciphertext)).toEqual(new Uint8Array(0));
  });

  it("encryptWithKey lets a caller reuse one key (thumbnail reuse path)", async () => {
    const key = crypto.getRandomValues(new Uint8Array(32));
    const full = await encryptWithKey(key, pattern(2000));
    const thumb = await encryptWithKey(key, pattern(64));
    expect(await decryptMedia(key, full)).toEqual(pattern(2000));
    expect(await decryptMedia(key, thumb)).toEqual(pattern(64));
  });
});
