import { describe, expect, it } from "vitest";
import { GroupCallCrypto } from "./groupCallCrypto";

const enc = (s: string) => new TextEncoder().encode(s);
const dec = (b: Uint8Array) => new TextDecoder().decode(b);
const root = new Uint8Array(32).fill(7); // shared group root (both ends derive alike)

function indexOfBytes(haystack: Uint8Array, needle: Uint8Array): number {
  outer: for (let i = 0; i + needle.length <= haystack.length; i++) {
    for (let j = 0; j < needle.length; j++) if (haystack[i + j] !== needle[j]) continue outer;
    return i;
  }
  return -1;
}

describe("GroupCallCrypto", () => {
  it("each member seals once; every other member opens with that sender's key", async () => {
    const a = new GroupCallCrypto(root, { roomId: "r", selfId: "A" }, ["B", "C"]);
    const b = new GroupCallCrypto(root, { roomId: "r", selfId: "B" }, ["A", "C"]);
    const c = new GroupCallCrypto(root, { roomId: "r", selfId: "C" }, ["A", "B"]);
    await Promise.all([a.start(0), b.start(0), c.start(0)]);

    const sealed = await a.seal(enc("hello group"));
    expect(dec(await b.openFrom("A", sealed))).toBe("hello group");
    expect(dec(await c.openFrom("A", sealed))).toBe("hello group");
    expect(indexOfBytes(sealed, enc("hello group"))).toBe(-1); // no plaintext on the wire
  });

  it("rejects a frame opened against the wrong sender / an unknown peer", async () => {
    const a = new GroupCallCrypto(root, { roomId: "r", selfId: "A" }, ["B"]);
    const b = new GroupCallCrypto(root, { roomId: "r", selfId: "B" }, ["A"]);
    await Promise.all([a.start(0), b.start(0)]);
    const sealed = await a.seal(enc("hi"));
    await expect(b.openFrom("Z", sealed)).rejects.toThrow(/no key for peer/);
  });

  it("a joiner (epoch bump) reads new frames but the roster/epoch advance", async () => {
    const a = new GroupCallCrypto(root, { roomId: "r", selfId: "A" }, ["B"]);
    const d = new GroupCallCrypto(root, { roomId: "r", selfId: "D" }, ["A", "B"]);
    await a.start(0);
    await a.memberJoined("D", 1); // call-ctl signals epoch 1 on join
    await d.start(1);
    expect(a.currentEpoch()).toBe(1);
    expect(a.roster()).toContain("D");

    const sealed = await a.seal(enc("after D joined"));
    expect(dec(await d.openFrom("A", sealed))).toBe("after D joined");
  });

  it("forward secrecy: a member who left cannot open the next epoch", async () => {
    const a = new GroupCallCrypto(root, { roomId: "r", selfId: "A" }, ["B", "C"]);
    const c = new GroupCallCrypto(root, { roomId: "r", selfId: "C" }, ["A", "B"]);
    await Promise.all([a.start(0), c.start(0)]);

    await a.memberLeft("C", 1); // C removed; A rotates to epoch 1
    expect(a.roster()).not.toContain("C");
    const sealed = await a.seal(enc("secret after C left"));
    // C is stuck at epoch 0 — it has no epoch-1 key for A.
    await expect(c.openFrom("A", sealed)).rejects.toThrow();
  });

  it("keeps the previous epoch one generation so in-flight frames still open", async () => {
    const a = new GroupCallCrypto(root, { roomId: "r2", selfId: "A" }, ["B"]);
    const b = new GroupCallCrypto(root, { roomId: "r2", selfId: "B" }, ["A"]);
    await Promise.all([a.start(0), b.start(0)]);

    const inflight = await a.seal(enc("in flight")); // sealed at epoch 0
    await Promise.all([a.rotate(1), b.rotate(1)]); // both advance
    expect(dec(await b.openFrom("A", inflight))).toBe("in flight"); // epoch 0 retained

    await Promise.all([a.rotate(2), b.rotate(2)]); // advance again → epoch 0 retired
    await expect(b.openFrom("A", inflight)).rejects.toThrow();
  });
});
