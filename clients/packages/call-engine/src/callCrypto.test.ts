import { describe, expect, it } from "vitest";
import { CallCrypto } from "./callCrypto";

const bytes = (...v: number[]) => Uint8Array.from(v);

// Both ends share the Signal-session root secret; each derives its own send key
// (by sender id) and the peer's key for decrypt — nothing key-related crosses
// the wire.
function pair(secret: Uint8Array, roomId: string) {
  const alice = new CallCrypto(secret, { roomId, selfId: "alice", peerId: "bob" });
  const bob = new CallCrypto(secret, { roomId, selfId: "bob", peerId: "alice" });
  return { alice, bob };
}

describe("CallCrypto", () => {
  it("lets each peer decrypt the other's sealed frames", async () => {
    const { alice, bob } = pair(new Uint8Array(32).fill(9), "call-xyz");
    await alice.start();
    await bob.start();

    const fromAlice = await alice.seal(bytes(1, 2, 3, 4));
    expect(await bob.open(fromAlice)).toEqual(bytes(1, 2, 3, 4));

    const fromBob = await bob.seal(bytes(5, 6, 7));
    expect(await alice.open(fromBob)).toEqual(bytes(5, 6, 7));
  });

  it("advances the epoch on rotate and still opens the previous epoch's frames", async () => {
    const { alice, bob } = pair(new Uint8Array(32).fill(3), "call-rot");
    await alice.start();
    await bob.start();
    expect(alice.currentEpoch()).toBe(0);

    const beforeRotate = await alice.seal(bytes(1, 1)); // epoch 0

    await alice.rotate(1);
    await bob.rotate(1);
    expect(alice.currentEpoch()).toBe(1);

    const afterRotate = await alice.seal(bytes(2, 2)); // epoch 1
    expect(await bob.open(beforeRotate)).toEqual(bytes(1, 1)); // previous epoch still opens
    expect(await bob.open(afterRotate)).toEqual(bytes(2, 2));
  });

  it("a mismatched root secret cannot decrypt", async () => {
    const a = new CallCrypto(new Uint8Array(32).fill(1), { roomId: "r", selfId: "alice", peerId: "bob" });
    const b = new CallCrypto(new Uint8Array(32).fill(2), { roomId: "r", selfId: "bob", peerId: "alice" });
    await a.start();
    await b.start();
    await expect(b.open(await a.seal(bytes(1, 2, 3)))).rejects.toBeTruthy();
  });
});
