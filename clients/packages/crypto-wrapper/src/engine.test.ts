import { describe, expect, it } from "vitest";
import { DevSessionCipher, devBundle, generateDevIdentity, type DeviceSecret } from "./devSession";
import { E2EEEngine, InMemoryKeyDirectory, UntrustedDeviceError, trustAll, type DeviceTrust } from "./engine";

const enc = (s: string) => new TextEncoder().encode(s);
const dec = (b: Uint8Array) => new TextDecoder().decode(b);
const seed = (n: number) => new Uint8Array([n, n * 2 + 1, n * 3 + 2, n + 7]);

function engineFor(id: DeviceSecret, dir: InMemoryKeyDirectory, trust: DeviceTrust = trustAll): E2EEEngine {
  return new E2EEEngine(new DevSessionCipher(id), dir, trust, id.address);
}

describe("E2EEEngine", () => {
  it("P14: Alice→Bob — the relayed bytes carry no plaintext, Bob decrypts", async () => {
    const dir = new InMemoryKeyDirectory();
    const alice = generateDevIdentity({ userId: "alice", deviceId: "a1" }, seed(1));
    const bob = generateDevIdentity({ userId: "bob", deviceId: "b1" }, seed(2));
    dir.add(devBundle(alice));
    dir.add(devBundle(bob));

    const pt = enc("meet at the docks at midnight");
    const sealed = await engineFor(alice, dir).seal("bob", pt);
    expect(sealed).toHaveLength(1); // bob's single device
    const env = sealed[0]!.envelope;
    expect(sealed[0]!.selfSync).toBe(false);

    expect(indexOfBytes(env, pt)).toBe(-1); // ← P14: server sees only opaque bytes
    const opened = await engineFor(bob, dir).open(alice.address, env);
    expect(dec(opened.content)).toBe("meet at the docks at midnight");
    expect(opened).toMatchObject({ selfSync: false, conversationId: "alice" });
  });

  it("fans out to recipient devices AND the sender's other devices (self-sync)", async () => {
    const dir = new InMemoryKeyDirectory();
    const a1 = generateDevIdentity({ userId: "alice", deviceId: "a1" }, seed(1));
    const a2 = generateDevIdentity({ userId: "alice", deviceId: "a2" }, seed(3));
    const bob = generateDevIdentity({ userId: "bob", deviceId: "b1" }, seed(2));
    [a1, a2, bob].forEach((id) => dir.add(devBundle(id)));

    const pt = enc("hello from a1");
    const sealed = await engineFor(a1, dir).seal("bob", pt);
    expect(sealed.map((s) => s.address.deviceId).sort()).toEqual(["a2", "b1"]); // not a1 itself

    const forBob = sealed.find((s) => s.address.deviceId === "b1")!;
    const forA2 = sealed.find((s) => s.address.deviceId === "a2")!;
    expect(forBob.selfSync).toBe(false);
    expect(forA2.selfSync).toBe(true);

    // Bob sees a direct message; the conversation is the sender (alice).
    const bobGot = await engineFor(bob, dir).open(a1.address, forBob.envelope);
    expect(dec(bobGot.content)).toBe("hello from a1");
    expect(bobGot).toMatchObject({ selfSync: false, conversationId: "alice" });

    // Alice's second device sees the sent message via the self-sync copy — same
    // content, but the conversation is the recipient (bob) it was sent to.
    const a2Got = await engineFor(a2, dir).open(a1.address, forA2.envelope);
    expect(dec(a2Got.content)).toBe("hello from a1");
    expect(a2Got).toMatchObject({ selfSync: true, sentTo: "bob", conversationId: "bob" });
  });

  it("never encrypts to a device that is not on a trusted device list", async () => {
    const dir = new InMemoryKeyDirectory();
    const alice = generateDevIdentity({ userId: "alice", deviceId: "a1" }, seed(1));
    const b1 = generateDevIdentity({ userId: "bob", deviceId: "b1" }, seed(2));
    const rogue = generateDevIdentity({ userId: "bob", deviceId: "rogue" }, seed(9));
    [alice, b1, rogue].forEach((id) => dir.add(devBundle(id)));

    // A server-inserted rogue device is not on bob's trusted list.
    const trustExceptRogue: DeviceTrust = { isTrusted: (a) => a.deviceId !== "rogue" };
    const sealed = await engineFor(alice, dir, trustExceptRogue).seal("bob", enc("secret"));
    expect(sealed.map((s) => s.address.deviceId)).toEqual(["b1"]); // rogue skipped — never sealed to
  });

  it("rejects a sender device not on a trusted device list", async () => {
    const dir = new InMemoryKeyDirectory();
    const alice = generateDevIdentity({ userId: "alice", deviceId: "a1" }, seed(1));
    const bob = generateDevIdentity({ userId: "bob", deviceId: "b1" }, seed(2));
    dir.add(devBundle(alice));
    dir.add(devBundle(bob));

    const env = (await engineFor(alice, dir).seal("bob", enc("hi")))[0]!.envelope;
    const distrustAlice: DeviceTrust = { isTrusted: (a) => a.userId !== "alice" };
    await expect(engineFor(bob, dir, distrustAlice).open(alice.address, env)).rejects.toBeInstanceOf(
      UntrustedDeviceError,
    );
  });

  it("reuses the established session for later messages", async () => {
    const dir = new InMemoryKeyDirectory();
    const alice = generateDevIdentity({ userId: "alice", deviceId: "a1" }, seed(1));
    const bob = generateDevIdentity({ userId: "bob", deviceId: "b1" }, seed(2));
    dir.add(devBundle(alice));
    dir.add(devBundle(bob));

    const aliceEngine = engineFor(alice, dir);
    const bobEngine = engineFor(bob, dir);
    const e1 = (await aliceEngine.seal("bob", enc("one")))[0]!.envelope;
    const e2 = (await aliceEngine.seal("bob", enc("two")))[0]!.envelope;
    expect(dec((await bobEngine.open(alice.address, e1)).content)).toBe("one");
    expect(dec((await bobEngine.open(alice.address, e2)).content)).toBe("two");
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
