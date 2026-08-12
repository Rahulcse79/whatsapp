import { describe, expect, it } from "vitest";
import {
  createDevSignatureScheme,
  SignedDeviceListTrust,
  signDeviceList,
  verifyDeviceList,
  type DeviceInput,
} from "./deviceList";

const scheme = createDevSignatureScheme();
const K = (b: number) => new Uint8Array(32).fill(b); // a stand-in identity key
// Dev double is symmetric: the "public" key equals the signing key.
const primaryA = K(0xa1);
const primaryB = K(0xb2);

const devices: DeviceInput[] = [
  { deviceId: "d-primary", identityKey: K(1) },
  { deviceId: "d-phone", identityKey: K(2) },
];

describe("signDeviceList / verifyDeviceList", () => {
  it("round-trips: a list signed by the primary verifies", () => {
    const list = signDeviceList(scheme, primaryA, primaryA, "alice", 1, devices);
    expect(verifyDeviceList(scheme, list)).toBe(true);
    expect(list.devices.map((d) => d.deviceId)).toEqual(["d-primary", "d-phone"]);
  });

  it("rejects a tampered device cert", () => {
    const list = signDeviceList(scheme, primaryA, primaryA, "alice", 1, devices);
    const cert = list.devices[1]!.cert;
    cert[0] = cert[0]! ^ 0xff; // flip a byte in d-phone's cert
    expect(verifyDeviceList(scheme, list)).toBe(false);
  });

  it("rejects a swapped device identity key (cert no longer binds)", () => {
    const list = signDeviceList(scheme, primaryA, primaryA, "alice", 1, devices);
    list.devices[1]!.identityKey = K(99); // server swaps a device's key
    expect(verifyDeviceList(scheme, list)).toBe(false);
  });

  it("rejects verification under the wrong primary key", () => {
    const list = signDeviceList(scheme, primaryA, primaryA, "alice", 1, devices);
    list.primaryKey = primaryB; // claim a different primary
    expect(verifyDeviceList(scheme, list)).toBe(false);
  });
});

describe("SignedDeviceListTrust", () => {
  it("trusts devices on a learned, verified list and nothing else", () => {
    const trust = new SignedDeviceListTrust(scheme);
    expect(trust.learn(signDeviceList(scheme, primaryA, primaryA, "alice", 1, devices))).toBe(true);
    expect(trust.isTrusted({ userId: "alice", deviceId: "d-phone" })).toBe(true);
    expect(trust.isTrusted({ userId: "alice", deviceId: "d-unknown" })).toBe(false);
    expect(trust.isTrusted({ userId: "bob", deviceId: "d-phone" })).toBe(false);
  });

  it("accepts an add/remove update under the same primary key", () => {
    const trust = new SignedDeviceListTrust(scheme);
    trust.learn(signDeviceList(scheme, primaryA, primaryA, "alice", 1, devices));
    const withLaptop = [...devices, { deviceId: "d-laptop", identityKey: K(3) }];
    expect(trust.learn(signDeviceList(scheme, primaryA, primaryA, "alice", 2, withLaptop))).toBe(true);
    expect(trust.isTrusted({ userId: "alice", deviceId: "d-laptop" })).toBe(true);
  });

  it("rejects a list under a DIFFERENT primary key (the key-change warning)", () => {
    const trust = new SignedDeviceListTrust(scheme);
    trust.learn(signDeviceList(scheme, primaryA, primaryA, "alice", 1, devices));
    // A rogue server re-signs alice's list with its own primary key.
    const rogue = signDeviceList(scheme, primaryB, primaryB, "alice", 2, [
      { deviceId: "d-evil", identityKey: K(66) },
    ]);
    expect(trust.learn(rogue)).toBe(false); // pinned primary changed → rejected
    expect(trust.isTrusted({ userId: "alice", deviceId: "d-evil" })).toBe(false);
  });

  it("does not learn an unverifiable list", () => {
    const trust = new SignedDeviceListTrust(scheme);
    const list = signDeviceList(scheme, primaryA, primaryA, "alice", 1, devices);
    list.signature[0] = list.signature[0]! ^ 0xff; // corrupt the list signature
    expect(trust.learn(list)).toBe(false);
    expect(trust.isTrusted({ userId: "alice", deviceId: "d-phone" })).toBe(false);
  });
});
