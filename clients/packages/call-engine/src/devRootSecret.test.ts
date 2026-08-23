import { describe, expect, it } from "vitest";
import { CallCrypto } from "./callCrypto";
import { createDevRootSecretProvider } from "./devRootSecret";

// These tests exist because of a real bug: the web and mobile CallContexts passed
// the DEVICE id as `selfId` while `peerId` was always a USER id. The crypto was
// fine and its own unit tests passed — they paired the ids correctly — but the
// wiring put the two ends in different identity spaces, so neither the root
// secret nor the per-sender frame keys matched. Every inbound frame failed to
// open and was dropped, and the call connected with no audio and no video.
//
// The rule these lock in: BOTH ends must key the call on the same identity
// space, and it must be the one `peerId` is expressed in (user ids).

const ROOM = "room-1";

describe("dev root secret", () => {
  it("agrees between peers that use the same identity space", async () => {
    const alice = createDevRootSecretProvider("user-alice", "seed");
    const bob = createDevRootSecretProvider("user-bob", "seed");

    const a = await alice.callSecret("user-bob", ROOM);
    const b = await bob.callSecret("user-alice", ROOM);

    expect(Array.from(a)).toEqual(Array.from(b)); // order-independent pairing
  });

  // The regression itself: alice keys on her device id, bob keys on his, and
  // each names the other by user id.
  it("does NOT agree when one end keys on a device id and the other on a user id", async () => {
    const alice = createDevRootSecretProvider("device-alice", "seed");
    const bob = createDevRootSecretProvider("device-bob", "seed");

    const a = await alice.callSecret("user-bob", ROOM);
    const b = await bob.callSecret("user-alice", ROOM);

    expect(Array.from(a)).not.toEqual(Array.from(b));
  });

  it("derives a different secret per room, so one call's key cannot open another's", async () => {
    const alice = createDevRootSecretProvider("user-alice", "seed");
    const first = await alice.callSecret("user-bob", "room-1");
    const second = await alice.callSecret("user-bob", "room-2");
    expect(Array.from(first)).not.toEqual(Array.from(second));
  });
});

describe("identity-space mismatch end to end", () => {
  // Proves the user-visible symptom: with mismatched ids the peers can still
  // "connect" and seal frames, but nothing the other sends can be opened — which
  // in the app means a live call carrying no media.
  it("produces frames the peer cannot open", async () => {
    const aliceProv = createDevRootSecretProvider("device-alice", "seed");
    const bobProv = createDevRootSecretProvider("device-bob", "seed");
    const aliceSecret = await aliceProv.callSecret("user-bob", ROOM);
    const bobSecret = await bobProv.callSecret("user-alice", ROOM);

    const alice = new CallCrypto(aliceSecret, { roomId: ROOM, selfId: "device-alice", peerId: "user-bob" });
    const bob = new CallCrypto(bobSecret, { roomId: ROOM, selfId: "device-bob", peerId: "user-alice" });
    await alice.start();
    await bob.start();

    const sealed = await alice.seal(Uint8Array.from([1, 2, 3]));
    await expect(bob.open(sealed)).rejects.toThrow();
  });

  // And the fix: one identity space on both ends restores media.
  it("interoperates once both ends use the user id", async () => {
    const aliceProv = createDevRootSecretProvider("user-alice", "seed");
    const bobProv = createDevRootSecretProvider("user-bob", "seed");
    const secretA = await aliceProv.callSecret("user-bob", ROOM);
    const secretB = await bobProv.callSecret("user-alice", ROOM);
    expect(Array.from(secretA)).toEqual(Array.from(secretB));

    const alice = new CallCrypto(secretA, { roomId: ROOM, selfId: "user-alice", peerId: "user-bob" });
    const bob = new CallCrypto(secretB, { roomId: ROOM, selfId: "user-bob", peerId: "user-alice" });
    await alice.start();
    await bob.start();

    const fromAlice = await alice.seal(Uint8Array.from([1, 2, 3]));
    expect(await bob.open(fromAlice)).toEqual(Uint8Array.from([1, 2, 3]));

    const fromBob = await bob.seal(Uint8Array.from([9, 8]));
    expect(await alice.open(fromBob)).toEqual(Uint8Array.from([9, 8]));
  });
});
