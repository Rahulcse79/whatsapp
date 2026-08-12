// Protocol scenario P6 (test-strategy.md §3) — client half: a 1,024-member group
// where a member is removed and the Sender-Key rotation must apply BEFORE the
// next message, ordered by event version. (The server half — 1,024-way fan-out +
// aggregate receipts — lives in server/internal/fanout/scenario_p6_test.go.)

import { describe, expect, it } from "vitest";
import { GroupSession } from "./senderKey";

const enc = (s: string) => new TextEncoder().encode(s);
const dec = (b: Uint8Array) => new TextDecoder().decode(b);

describe("P6: 1,024-member group Sender-Key rotation ordering", () => {
  const GROUP = "g1024";
  // 1,024 members: alice (the sender) + m0..m1022.
  const members = ["alice", ...Array.from({ length: 1023 }, (_, i) => `m${i}`)];
  const REMOVED = "m1022";

  it("rotates before the next message so a removed member cannot read forward", () => {
    const alice = new GroupSession("alice");
    const stays = new GroupSession("m0"); // a member who remains
    const removed = new GroupSession(REMOVED); // the member to be removed
    stays.acceptDistribution(GROUP, "alice", alice.distribution(GROUP));
    removed.acceptDistribution(GROUP, "alice", alice.distribution(GROUP));

    // Before removal both members read the same fanned-out ciphertext.
    const e1 = alice.encrypt(GROUP, enc("before removal"));
    expect(dec(stays.decrypt(GROUP, "alice", e1))).toBe("before removal");
    expect(dec(removed.decrypt(GROUP, "alice", e1))).toBe("before removal");

    // member_removed(version 1): rotate our key, redistribute to the 1,022 other
    // remaining members, drop the removed one.
    const remaining = members.filter((m) => m !== REMOVED); // 1,023 incl. alice
    const plan = alice.applyGroupEvent(GROUP, { kind: "member_removed", subject: REMOVED, version: 1 }, remaining);
    expect(plan.rotated).toBe(true);
    expect(plan.dropped).toEqual([REMOVED]);
    expect(plan.distributeTo).toHaveLength(1022); // remaining minus self
    expect(plan.distributeTo).not.toContain("alice");
    expect(plan.distributeTo).not.toContain(REMOVED);

    // The fresh key reaches the remaining member; the next message uses it.
    stays.acceptDistribution(GROUP, "alice", alice.distribution(GROUP));
    const e2 = alice.encrypt(GROUP, enc("after removal"));
    expect(dec(stays.decrypt(GROUP, "alice", e2))).toBe("after removal");
    // The removed member holds only the pre-rotation key → forward secrecy.
    expect(() => removed.decrypt(GROUP, "alice", e2)).toThrow();
  });

  it("is ordered by version: a stale/replayed event is ignored (no re-rotation)", () => {
    const alice = new GroupSession("alice");
    const stays = new GroupSession("m0");
    stays.acceptDistribution(GROUP, "alice", alice.distribution(GROUP));

    const remaining = members.filter((m) => m !== REMOVED);
    alice.applyGroupEvent(GROUP, { kind: "member_removed", subject: REMOVED, version: 1 }, remaining);
    stays.acceptDistribution(GROUP, "alice", alice.distribution(GROUP));

    // Replaying the same version (≤ last applied) must not rotate again — the key
    // the remaining member holds stays valid.
    const replay = alice.applyGroupEvent(GROUP, { kind: "member_removed", subject: REMOVED, version: 1 }, remaining);
    expect(replay.rotated).toBe(false);

    const e = alice.encrypt(GROUP, enc("same key still"));
    expect(dec(stays.decrypt(GROUP, "alice", e))).toBe("same key still");
  });
});
