import { describe, expect, it } from "vitest";
import { FrameCryptor } from "./frameCrypto";

const k = (fill: number) => new Uint8Array(32).fill(fill);
const bytes = (...v: number[]) => Uint8Array.from(v);

function contains(hay: Uint8Array, needle: Uint8Array): boolean {
  outer: for (let i = 0; i + needle.length <= hay.length; i++) {
    for (let j = 0; j < needle.length; j++) if (hay[i + j] !== needle[j]) continue outer;
    return true;
  }
  return false;
}

/** A loopback cryptor: same key installed for send and receive under keyId. */
async function loopback(keyId: number, raw: Uint8Array): Promise<FrameCryptor> {
  const c = new FrameCryptor();
  await c.setSendKey(keyId, raw);
  await c.addRecvKey(keyId, raw);
  return c;
}

describe("FrameCryptor", () => {
  it("round-trips a frame", async () => {
    const c = await loopback(0, k(1));
    const frame = bytes(9, 8, 7, 6, 5, 4, 3, 2, 1, 0);
    const opened = await c.open(await c.seal(frame));
    expect(opened).toEqual(frame);
  });

  it("does not leak plaintext and never repeats a sealed frame", async () => {
    const c = await loopback(0, k(2));
    const marker = new Uint8Array(64).fill(0xab);
    const a = await c.seal(marker);
    const b = await c.seal(marker); // same plaintext, next counter
    expect(contains(a, marker)).toBe(false);
    expect(a).not.toEqual(b); // counter advanced → different nonce → different ciphertext
  });

  it("carries keyId + counter in the cleartext header", async () => {
    const c = new FrameCryptor();
    await c.setSendKey(7, k(3));
    const first = await c.seal(bytes(1, 2, 3));
    const second = await c.seal(bytes(1, 2, 3));
    expect(first[0]).toBe(7); // keyId
    expect(first[8]).toBe(0); // counter 0 (low byte)
    expect(second[8]).toBe(1); // counter 1
  });

  it("rejects a frame under an unknown key id", async () => {
    const c = new FrameCryptor();
    await c.setSendKey(1, k(4));
    const sealed = await c.seal(bytes(1, 2, 3)); // keyId 1, but no recv key installed
    await expect(c.open(sealed)).rejects.toThrow(/no key for keyId/);
  });

  it("rejects a wrong key (GCM tag fails)", async () => {
    const enc = new FrameCryptor();
    await enc.setSendKey(0, k(5));
    const dec = new FrameCryptor();
    await dec.addRecvKey(0, k(6)); // different key
    await expect(dec.open(await enc.seal(bytes(1, 2, 3)))).rejects.toBeTruthy();
  });

  it("rejects a tampered header (AAD) or body", async () => {
    const c = await loopback(0, k(7));
    const sealed = await c.seal(new Uint8Array(32).fill(1));

    const flippedBody = Uint8Array.from(sealed);
    const last = flippedBody.length - 1;
    flippedBody[last] = (flippedBody[last]! ^ 0x01) & 0xff;
    await expect(c.open(flippedBody)).rejects.toBeTruthy();

    const flippedHeader = Uint8Array.from(sealed);
    flippedHeader[8] = (flippedHeader[8]! ^ 0x01) & 0xff; // counter in header ≠ nonce used
    await expect(c.open(flippedHeader)).rejects.toBeTruthy();
  });

  it("keeps decrypting old-key frames across a send-key rotation", async () => {
    const c = new FrameCryptor();
    await c.setSendKey(0, k(8));
    await c.addRecvKey(0, k(8));
    const old = await c.seal(bytes(1, 1, 1)); // sealed under key 0

    // Rotate to key 1 (new epoch), keeping recv key 0 for in-flight frames.
    await c.setSendKey(1, k(9));
    await c.addRecvKey(1, k(9));
    const fresh = await c.seal(bytes(2, 2, 2)); // sealed under key 1

    expect(await c.open(old)).toEqual(bytes(1, 1, 1)); // old key still opens
    expect(await c.open(fresh)).toEqual(bytes(2, 2, 2));

    c.removeRecvKey(0);
    await expect(c.open(old)).rejects.toThrow(/no key for keyId 0/);
  });

  it("rejects a key of the wrong length", async () => {
    const c = new FrameCryptor();
    await expect(c.setSendKey(0, new Uint8Array(16))).rejects.toThrow(/32 bytes/);
  });
});
