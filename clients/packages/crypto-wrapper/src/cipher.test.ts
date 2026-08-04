import { describe, it, expect } from "vitest";
import { MockSessionCipher, type DeviceAddress, type PrekeyBundle } from "./cipher";

const alice: DeviceAddress = { userId: "alice", deviceId: "d1" };
const bob: DeviceAddress = { userId: "bob", deviceId: "d1" };

function bundle(addr: DeviceAddress, id: number): PrekeyBundle {
  return {
    address: addr,
    identityKey: new Uint8Array([id, id + 1, id + 2]),
    signedPrekey: new Uint8Array([9, 8, 7]),
    oneTimePrekey: new Uint8Array([id]),
  };
}

const text = new TextEncoder();
const dec = new TextDecoder();

describe("SessionCipher contract", () => {
  it("round-trips a message through an established session", async () => {
    const c = new MockSessionCipher();
    await c.establish(bundle(bob, 1));
    const plaintext = text.encode("hello bob");
    const sealed = await c.encrypt(bob, plaintext);
    const opened = await c.decrypt(bob, sealed);
    expect(dec.decode(opened)).toBe("hello bob");
  });

  it("produces opaque ciphertext (not the plaintext)", async () => {
    const c = new MockSessionCipher();
    await c.establish(bundle(bob, 1));
    const plaintext = text.encode("secret");
    const sealed = await c.encrypt(bob, plaintext);
    expect([...sealed]).not.toEqual([...plaintext]);
  });

  it("rejects encrypt/decrypt before a session is established", async () => {
    const c = new MockSessionCipher();
    expect(c.hasSession(bob)).toBe(false);
    await expect(c.encrypt(bob, text.encode("x"))).rejects.toThrow(/no session/);
    await expect(c.decrypt(bob, new Uint8Array([1]))).rejects.toThrow(/no session/);
  });

  it("keys sessions per device address", async () => {
    const c = new MockSessionCipher();
    await c.establish(bundle(bob, 1));
    expect(c.hasSession(bob)).toBe(true);
    expect(c.hasSession(alice)).toBe(false);
  });

  it("distinct bundles yield distinct keystreams", async () => {
    const c1 = new MockSessionCipher();
    const c2 = new MockSessionCipher();
    await c1.establish(bundle(bob, 1));
    await c2.establish(bundle(bob, 42));
    const p = text.encode("same message");
    const a = await c1.encrypt(bob, p);
    const b = await c2.encrypt(bob, p);
    expect([...a]).not.toEqual([...b]);
  });
});
