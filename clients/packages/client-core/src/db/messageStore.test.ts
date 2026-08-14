import { Cursors } from "@wa/sync-engine";
import { describe, expect, it } from "vitest";
import { MsgKind, type InboxBatch, type InboxItemFrame } from "../frames";
import { planInboxBatch } from "./messageStore";

function item(seq: number, over?: Partial<InboxItemFrame>): InboxItemFrame {
  return {
    conversationId: "c1",
    seq,
    msgUuid: `m${seq}`,
    senderUserId: "u2",
    senderDeviceId: "d2",
    kind: MsgKind.TEXT,
    ciphertext: new Uint8Array([seq]),
    acceptedAtMs: seq * 10,
    ...over,
  };
}

const batch = (items: InboxItemFrame[]): InboxBatch => ({ t: "inbox_batch", items });

describe("planInboxBatch", () => {
  it("inserts new messages, advances cursors, and reports the watermark", () => {
    const cursors = new Cursors();
    const plan = planInboxBatch(batch([item(1), item(2), item(3)]), cursors);
    expect(plan.inserts.map((i) => i.msgUuid)).toEqual(["m1", "m2", "m3"]);
    expect(plan.watermark).toEqual([{ conversationId: "c1", lastSeq: 3 }]);
    expect(cursors.get("c1")).toBe(3);
  });

  it("skips at-least-once duplicates (seq ≤ cursor) but still ACKs them", () => {
    const cursors = new Cursors();
    cursors.advance("c1", 5);
    const plan = planInboxBatch(batch([item(3), item(4), item(5)]), cursors);
    expect(plan.inserts).toHaveLength(0); // all already persisted
    expect(plan.watermark).toEqual([{ conversationId: "c1", lastSeq: 5 }]);
  });

  it("routes overlay kinds to overlays, not inserts", () => {
    const cursors = new Cursors();
    const plan = planInboxBatch(
      batch([
        item(1),
        item(2, { kind: MsgKind.OVERLAY_DELETE, overlayTarget: "m1", msgUuid: "o2" }),
      ]),
      cursors,
    );
    expect(plan.inserts.map((i) => i.msgUuid)).toEqual(["m1"]);
    expect(plan.overlays).toEqual([{ conversationId: "c1", targetMsgUuid: "m1", msgUuid: "o2", kind: MsgKind.OVERLAY_DELETE }]);
    expect(plan.watermark).toEqual([{ conversationId: "c1", lastSeq: 2 }]);
  });

  it("routes reaction and pin kinds to overlays too (never new bubbles)", () => {
    const cursors = new Cursors();
    const plan = planInboxBatch(
      batch([
        item(1),
        item(2, { kind: MsgKind.REACTION, overlayTarget: "m1", msgUuid: "r2" }),
        item(3, { kind: MsgKind.PIN, overlayTarget: "m1", msgUuid: "p3" }),
      ]),
      cursors,
    );
    expect(plan.inserts.map((i) => i.msgUuid)).toEqual(["m1"]); // only the real message
    expect(plan.overlays.map((o) => o.kind)).toEqual([MsgKind.REACTION, MsgKind.PIN]);
  });

  it("sorts out-of-order items by seq so the cursor gate is correct", () => {
    const cursors = new Cursors();
    const plan = planInboxBatch(batch([item(3), item(1), item(2)]), cursors);
    expect(plan.inserts.map((i) => i.seq)).toEqual([1, 2, 3]);
    expect(cursors.get("c1")).toBe(3);
  });

  it("tracks watermarks per conversation independently", () => {
    const cursors = new Cursors();
    const plan = planInboxBatch(
      batch([item(1, { conversationId: "a", msgUuid: "a1" }), item(1, { conversationId: "b", msgUuid: "b1" })]),
      cursors,
    );
    expect(plan.watermark).toEqual([
      { conversationId: "a", lastSeq: 1 },
      { conversationId: "b", lastSeq: 1 },
    ]);
  });
});
