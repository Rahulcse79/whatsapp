import { describe, it, expect } from "vitest";
import { applyOverlay, mergeReadSeq, type LocalMessage } from "./conflict";

function base(): LocalMessage {
  return { msgUuid: "m1", seq: 1, deleted: false, reactions: {} };
}

describe("applyOverlay", () => {
  it("delete is terminal and wins over a later edit", () => {
    let m = base();
    m = applyOverlay(m, { kind: "delete" });
    m = applyOverlay(m, { kind: "edit", editBody: "too late" });
    expect(m.deleted).toBe(true);
    expect(m.editedBody).toBeUndefined();
  });

  it("edit applies to a live message; latest wins", () => {
    let m = base();
    m = applyOverlay(m, { kind: "edit", editBody: "v1" });
    m = applyOverlay(m, { kind: "edit", editBody: "v2" });
    expect(m.editedBody).toBe("v2");
    expect(m.deleted).toBe(false);
  });

  it("reactions are a set-union keyed by (emoji, reactor); adds are idempotent", () => {
    let m = base();
    m = applyOverlay(m, { kind: "reaction-add", emoji: "👍", reactorUserId: "u1" });
    m = applyOverlay(m, { kind: "reaction-add", emoji: "👍", reactorUserId: "u1" }); // dup
    m = applyOverlay(m, { kind: "reaction-add", emoji: "👍", reactorUserId: "u2" });
    expect([...(m.reactions["👍"] ?? [])].sort()).toEqual(["u1", "u2"]);
  });

  it("reaction removal tombstones the pair and prunes empty emoji sets", () => {
    let m = base();
    m = applyOverlay(m, { kind: "reaction-add", emoji: "❤️", reactorUserId: "u1" });
    m = applyOverlay(m, { kind: "reaction-remove", emoji: "❤️", reactorUserId: "u1" });
    expect(m.reactions["❤️"]).toBeUndefined();
  });

  it("applying overlays is order-independent for commutative reactions", () => {
    const forward = [
      { kind: "reaction-add", emoji: "🎉", reactorUserId: "a" },
      { kind: "reaction-add", emoji: "🎉", reactorUserId: "b" },
    ] as const;
    let m1 = base();
    for (const o of forward) m1 = applyOverlay(m1, o);
    let m2 = base();
    for (const o of [...forward].reverse()) m2 = applyOverlay(m2, o);
    expect([...(m1.reactions["🎉"] ?? [])].sort()).toEqual([...(m2.reactions["🎉"] ?? [])].sort());
  });
});

describe("mergeReadSeq", () => {
  it("advances monotonically (max) across a user's devices", () => {
    expect(mergeReadSeq(5, 9)).toBe(9);
    expect(mergeReadSeq(9, 5)).toBe(9);
    expect(mergeReadSeq(0, 0)).toBe(0);
  });
});

describe("applyOverlay pin/star", () => {
  it("toggles the pin flag", () => {
    let m = base();
    expect(m.pinned).toBeFalsy();
    m = applyOverlay(m, { kind: "pin" });
    expect(m.pinned).toBe(true);
    m = applyOverlay(m, { kind: "unpin" });
    expect(m.pinned).toBe(false);
  });

  it("toggles the star flag independently of pin", () => {
    let m = applyOverlay(base(), { kind: "star" });
    expect(m.starred).toBe(true);
    expect(m.pinned).toBeFalsy();
    m = applyOverlay(m, { kind: "pin" });
    expect(m.starred).toBe(true);
    expect(m.pinned).toBe(true);
    m = applyOverlay(m, { kind: "unstar" });
    expect(m.starred).toBe(false);
    expect(m.pinned).toBe(true);
  });

  it("pin/star survive a reaction and vice-versa (independent fields)", () => {
    let m = applyOverlay(base(), { kind: "pin" });
    m = applyOverlay(m, { kind: "reaction-add", emoji: "👍", reactorUserId: "u1" });
    expect(m.pinned).toBe(true);
    expect([...(m.reactions["👍"] ?? [])]).toEqual(["u1"]);
  });
});
