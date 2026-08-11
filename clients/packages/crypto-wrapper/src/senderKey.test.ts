import { describe, expect, it } from "vitest";
import { GroupSession } from "./senderKey";

const enc = (s: string) => new TextEncoder().encode(s);
const dec = (b: Uint8Array) => new TextDecoder().decode(b);

describe("GroupSession", () => {
  it("encrypts once and any member with the distribution decrypts", () => {
    const alice = new GroupSession("alice");
    const bob = new GroupSession("bob");
    bob.acceptDistribution("g1", "alice", alice.distribution("g1"));

    const env = alice.encrypt("g1", enc("hi group"));
    expect(dec(bob.decrypt("g1", "alice", env))).toBe("hi group");
    expect(indexOfBytes(env, enc("hi group"))).toBe(-1); // no plaintext on the wire
  });

  it("member_removed rotates the sender key so the removed member can't read forward", () => {
    const alice = new GroupSession("alice");
    const bob = new GroupSession("bob");
    const eve = new GroupSession("eve");
    bob.acceptDistribution("g1", "alice", alice.distribution("g1"));
    eve.acceptDistribution("g1", "alice", alice.distribution("g1"));

    const plan = alice.applyGroupEvent("g1", { kind: "member_removed", subject: "eve", version: 1 }, ["alice", "bob"]);
    expect(plan.rotated).toBe(true);
    expect(plan.distributeTo).toEqual(["bob"]);
    expect(plan.dropped).toEqual(["eve"]);

    // Alice redistributes the fresh key to the remaining member.
    bob.acceptDistribution("g1", "alice", alice.distribution("g1"));
    const env = alice.encrypt("g1", enc("secret after removal"));
    expect(dec(bob.decrypt("g1", "alice", env))).toBe("secret after removal");
    // Eve holds only the pre-rotation key → cannot read the new message.
    expect(() => eve.decrypt("g1", "alice", env)).toThrow();
  });

  it("member_added distributes the current key to the new member, no rotation", () => {
    const alice = new GroupSession("alice");
    const carol = new GroupSession("carol");
    const plan = alice.applyGroupEvent("g1", { kind: "member_added", subject: "carol", version: 1 }, ["alice", "carol"]);
    expect(plan.rotated).toBe(false);
    expect(plan.distributeTo).toEqual(["carol"]);

    carol.acceptDistribution("g1", "alice", alice.distribution("g1"));
    const env = alice.encrypt("g1", enc("welcome"));
    expect(dec(carol.decrypt("g1", "alice", env))).toBe("welcome");
  });

  it("ignores stale/duplicate events (ordered by version)", () => {
    const alice = new GroupSession("alice");
    alice.applyGroupEvent("g1", { kind: "member_added", subject: "x", version: 5 }, ["alice", "x"]);
    const stale = alice.applyGroupEvent("g1", { kind: "member_added", subject: "y", version: 5 }, ["alice", "y"]);
    expect(stale.distributeTo).toEqual([]);
    expect(stale.rotated).toBe(false);
  });

  it("role_changed is a no-op for keys", () => {
    const alice = new GroupSession("alice");
    const plan = alice.applyGroupEvent("g1", { kind: "role_changed", subject: "bob", version: 1 }, ["alice", "bob"]);
    expect(plan).toEqual({ rotated: false, distributeTo: [], dropped: [] });
  });

  it("self-removal forgets our own key and drops nothing to distribute", () => {
    const alice = new GroupSession("alice");
    alice.ensureOwn("g1");
    const plan = alice.applyGroupEvent("g1", { kind: "member_removed", subject: "alice", version: 1 }, []);
    expect(plan).toEqual({ rotated: false, distributeTo: [], dropped: ["alice"] });
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
