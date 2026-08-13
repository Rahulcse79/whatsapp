import { describe, expect, it } from "vitest";
import { createDevSignatureScheme, signDeviceList, SignedDeviceListTrust, type DeviceInput } from "./deviceList";
import { devBundle, DevSessionCipher, generateDevIdentity, type DeviceSecret } from "./devSession";
import { E2EEEngine, InMemoryKeyDirectory } from "./engine";

// Multi-device link/revoke suite (e2ee-design §5, T3.07). Exercises the whole
// flow through the signed-device-list trust: linking adds a device, revoking
// removes it durably (a replayed old list can't un-revoke it), a rogue primary
// key is a visible key change, and the message fan-out follows trust — a revoked
// device stops receiving copies.

const scheme = createDevSignatureScheme();
const seed = (n: number) => new Uint8Array([n, n * 2 + 1, n * 3 + 2, n + 7]);
const enc = (s: string) => new TextEncoder().encode(s);

function id(user: string, device: string, s: number): DeviceSecret {
  return generateDevIdentity({ userId: user, deviceId: device }, seed(s));
}
function input(d: DeviceSecret): DeviceInput {
  return { deviceId: d.address.deviceId, identityKey: d.publicKey };
}

describe("device link / revoke", () => {
  // Bob's primary (b1) signs his device list; b1 is on his own list.
  const b1 = id("bob", "b1", 1);
  const b2 = id("bob", "b2", 2);
  const primary = b1.publicKey; // dev double: private == public

  const listV1 = signDeviceList(scheme, primary, primary, "bob", 1, [input(b1)]);
  const listV2 = signDeviceList(scheme, primary, primary, "bob", 2, [input(b1), input(b2)]); // linked b2
  const listV3 = signDeviceList(scheme, primary, primary, "bob", 3, [input(b1)]); // revoked b2

  it("links a new device: it becomes trusted once the primary re-signs the list", () => {
    const trust = new SignedDeviceListTrust(scheme);
    expect(trust.learn(listV1)).toBe(true);
    expect(trust.isTrusted(b2.address)).toBe(false); // not yet linked

    expect(trust.learn(listV2)).toBe(true); // b2 linked
    expect(trust.isTrusted(b1.address)).toBe(true);
    expect(trust.isTrusted(b2.address)).toBe(true);
  });

  it("revokes a device, and a replayed older list cannot un-revoke it (downgrade)", () => {
    const trust = new SignedDeviceListTrust(scheme);
    trust.learn(listV1);
    trust.learn(listV2); // b1 + b2

    expect(trust.learn(listV3)).toBe(true); // revoke b2
    expect(trust.isTrusted(b2.address)).toBe(false);

    // A rogue server replays the pre-revocation list — rejected on version, so b2
    // stays revoked.
    expect(trust.learn(listV2)).toBe(false);
    expect(trust.isTrusted(b2.address)).toBe(false);
  });

  it("rejects a list under a different primary key (visible key change)", () => {
    const trust = new SignedDeviceListTrust(scheme);
    trust.learn(listV1); // pins b1's key as bob's primary

    const roguePrimary = id("bob", "rogue-primary", 5).publicKey;
    const rogue = id("bob", "rogue", 6);
    const rogueList = signDeviceList(scheme, roguePrimary, roguePrimary, "bob", 2, [input(b1), input(rogue)]);

    expect(trust.learn(rogueList)).toBe(false); // primary key mismatch → key-change warning
    expect(trust.isTrusted(rogue.address)).toBe(false); // rogue device never trusted
  });

  it("fan-out follows trust: a revoked device stops receiving message copies", async () => {
    const trust = new SignedDeviceListTrust(scheme);
    trust.learn(listV2); // bob has b1 + b2

    const dir = new InMemoryKeyDirectory();
    const a1 = id("alice", "a1", 9); // single-device sender → no self-sync copies
    [a1, b1, b2].forEach((d) => dir.add(devBundle(d)));
    const alice = new E2EEEngine(new DevSessionCipher(a1), dir, trust, a1.address);

    const before = await alice.seal("bob", enc("hi"));
    expect(before.map((s) => s.address.deviceId).sort()).toEqual(["b1", "b2"]);

    // Revoke b2, then send again — the fan-out now skips it (never encrypted to a
    // device off the trusted list).
    expect(trust.learn(listV3)).toBe(true);
    const after = await alice.seal("bob", enc("hi again"));
    expect(after.map((s) => s.address.deviceId)).toEqual(["b1"]);
  });
});
