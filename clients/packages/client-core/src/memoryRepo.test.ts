import { describe, expect, it } from "vitest";
import { MsgKind, type InboxBatch, type InboxItemFrame } from "./frames";
import { MemoryMessageRepo } from "./memoryRepo";

function inbound(seq: number, over?: Partial<InboxItemFrame>): InboxItemFrame {
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

describe("MemoryMessageRepo", () => {
  it("persists inbound messages, dedupes by msg_uuid, and returns the watermark", async () => {
    const repo = new MemoryMessageRepo();
    const w1 = await repo.persistInboxBatch(batch([inbound(1), inbound(2)]));
    expect(w1).toEqual([{ conversationId: "c1", lastSeq: 2 }]);
    // Replaying the same batch is a no-op (dedupe) and re-ACKs the watermark.
    await repo.persistInboxBatch(batch([inbound(1), inbound(2)]));
    const thread = await repo.thread("c1");
    expect(thread.map((m) => m.msgUuid)).toEqual(["m1", "m2"]);
    expect(repo.cursorSnapshot()).toEqual([{ conversationId: "c1", lastSeq: 2 }]);
  });

  it("applies delete overlays (delete-wins tombstone)", async () => {
    const repo = new MemoryMessageRepo();
    await repo.persistInboxBatch(batch([inbound(1)]));
    await repo.persistInboxBatch(
      batch([inbound(2, { kind: MsgKind.OVERLAY_DELETE, overlayTarget: "m1", msgUuid: "o2" })]),
    );
    const thread = await repo.thread("c1");
    const target = thread.find((m) => m.msgUuid === "m1");
    expect(target?.deleted).toBe(true);
    expect(target?.body).toBe("");
  });

  it("queues an outgoing message, exposes it as a MsgSend, and clears it on markSent", async () => {
    const repo = new MemoryMessageRepo();
    await repo.enqueueOutgoing({
      clientRef: "cr1",
      conversationId: "c1",
      plaintext: "hi",
      payload: new Uint8Array([9]),
      now: 100,
    });

    const pending = await repo.pendingSends();
    expect(pending).toHaveLength(1);
    expect(pending[0]).toMatchObject({ t: "msg_send", clientRef: "cr1", conversationId: "c1", kind: MsgKind.TEXT });

    const bubble = (await repo.thread("c1")).find((m) => m.msgUuid === "cr1");
    expect(bubble?.mine).toBe(true);
    expect(bubble?.state).toBe("sending");

    await repo.markSent("cr1", 7);
    expect(await repo.pendingSends()).toHaveLength(0);
    expect((await repo.thread("c1")).find((m) => m.msgUuid === "cr1")?.state).toBe("sent");
  });

  it("orders the conversation list by most-recent activity", async () => {
    const repo = new MemoryMessageRepo();
    await repo.enqueueOutgoing({ clientRef: "a", conversationId: "old", plaintext: "x", payload: new Uint8Array(), now: 1 });
    await repo.enqueueOutgoing({ clientRef: "b", conversationId: "new", plaintext: "y", payload: new Uint8Array(), now: 2 });
    const convs = await repo.conversations();
    expect(convs.map((c) => c.conversationId)).toEqual(["new", "old"]);
  });
});
