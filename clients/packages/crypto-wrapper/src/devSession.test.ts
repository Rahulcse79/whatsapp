import { describe, expect, it } from "vitest";
import { DecryptError, DevSessionCipher, devBundle, generateDevIdentity } from "./devSession";

const enc = (s: string) => new TextEncoder().encode(s);
const dec = (b: Uint8Array) => new TextDecoder().decode(b);
const seed = (n: number) => new Uint8Array([n, n * 2 + 1, n * 3 + 2, n + 7]);

function pair() {
  const alice = generateDevIdentity({ userId: "A", deviceId: "a1" }, seed(1));
  const bob = generateDevIdentity({ userId: "B", deviceId: "b1" }, seed(2));
  return { alice, bob, aliceCipher: new DevSessionCipher(alice), bobCipher: new DevSessionCipher(bob) };
}

describe("DevSessionCipher", () => {
  it("two parties derive the same session and round-trip a message", async () => {
    const { alice, bob, aliceCipher, bobCipher } = pair();
    await aliceCipher.establish(devBundle(bob));
    await bobCipher.establish(devBundle(alice));

    const env = await aliceCipher.encrypt(bob.address, enc("attack at dawn"));
    expect(dec(await bobCipher.decrypt(alice.address, env))).toBe("attack at dawn");
  });

  it("ratchets: the same plaintext twice yields different envelopes, both decrypt", async () => {
    const { alice, bob, aliceCipher, bobCipher } = pair();
    await aliceCipher.establish(devBundle(bob));
    await bobCipher.establish(devBundle(alice));

    const e1 = await aliceCipher.encrypt(bob.address, enc("hi"));
    const e2 = await aliceCipher.encrypt(bob.address, enc("hi"));
    expect([...e1]).not.toEqual([...e2]);
    expect(dec(await bobCipher.decrypt(alice.address, e1))).toBe("hi");
    expect(dec(await bobCipher.decrypt(alice.address, e2))).toBe("hi");
  });

  it("ciphertext contains no plaintext bytes", async () => {
    const { bob, aliceCipher } = pair();
    await aliceCipher.establish(devBundle(bob));
    const pt = enc("SECRETSECRET");
    expect(indexOfBytes(await aliceCipher.encrypt(bob.address, pt), pt)).toBe(-1);
  });

  it("a wrong session and a tampered envelope both fail authentication", async () => {
    const { alice, bob, aliceCipher, bobCipher } = pair();
    await aliceCipher.establish(devBundle(bob));
    const env = await aliceCipher.encrypt(bob.address, enc("hello"));

    // Eve derives a different root → cannot read Alice→Bob traffic.
    const eve = generateDevIdentity({ userId: "E", deviceId: "e1" }, seed(9));
    const eveCipher = new DevSessionCipher(eve);
    await eveCipher.establish(devBundle(alice));
    await expect(eveCipher.decrypt(alice.address, env)).rejects.toBeInstanceOf(DecryptError);

    // Flipping a ciphertext byte breaks the authentication tag.
    await bobCipher.establish(devBundle(alice));
    const tampered = env.slice();
    const last = tampered.length - 1;
    tampered[last] = (tampered[last]! ^ 0xff) & 0xff;
    await expect(bobCipher.decrypt(alice.address, tampered)).rejects.toBeInstanceOf(DecryptError);
  });

  it("rejects encrypt before a session exists", async () => {
    const { bob, aliceCipher } = pair();
    expect(aliceCipher.hasSession(bob.address)).toBe(false);
    await expect(aliceCipher.encrypt(bob.address, enc("x"))).rejects.toThrow(/no session/);
  });
});

function indexOfBytes(haystack: Uint8Array, needle: Uint8Array): number {
  outer: for (let i = 0; i + needle.length <= haystack.length; i++) {
    for (let j = 0; j < needle.length; j++) {
      if (haystack[i + j] !== needle[j]) continue outer;
    }
    return i;
  }
  return -1;
}
