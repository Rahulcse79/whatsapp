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

  it("sends an outgoing OVERLAY_DELETE: tombstones the target locally + queues the overlay (no new bubble)", async () => {
    const repo = new MemoryMessageRepo();
    await repo.enqueueOutgoing({ clientRef: "m1", conversationId: "c1", plaintext: "hi", payload: new Uint8Array([1]), now: 1 });
    await repo.enqueueOutgoing({ clientRef: "d1", conversationId: "c1", plaintext: "", payload: new Uint8Array([2]), now: 2, kind: MsgKind.OVERLAY_DELETE, overlayTarget: "m1" });
    const thread = await repo.thread("c1");
    expect(thread.map((m) => m.msgUuid)).toEqual(["m1"]); // overlay is not its own bubble
    expect(thread[0]!.deleted).toBe(true);
    const del = (await repo.pendingSends()).find((s) => s.clientRef === "d1");
    expect(del?.kind).toBe(MsgKind.OVERLAY_DELETE);
    expect(del?.overlayTarget).toBe("m1");
  });

  it("sends an outgoing OVERLAY_EDIT: rewrites the target body + marks it edited", async () => {
    const repo = new MemoryMessageRepo();
    await repo.enqueueOutgoing({ clientRef: "m1", conversationId: "c1", plaintext: "typo", payload: new Uint8Array([1]), now: 1 });
    await repo.enqueueOutgoing({ clientRef: "e1", conversationId: "c1", plaintext: "fixed", payload: new Uint8Array([2]), now: 2, kind: MsgKind.OVERLAY_EDIT, overlayTarget: "m1" });
    const thread = await repo.thread("c1");
    expect(thread).toHaveLength(1);
    expect(thread[0]!.body).toBe("fixed");
    expect(thread[0]!.edited).toBe(true);
  });

  it("applies an inbound OVERLAY_EDIT using the decrypted overlay body", async () => {
    const repo = new MemoryMessageRepo();
    await repo.persistInboxBatch(batch([inbound(1)]), new Map([["m1", "original"]]));
    await repo.persistInboxBatch(batch([inbound(2, { kind: MsgKind.OVERLAY_EDIT, overlayTarget: "m1", msgUuid: "o2" })]), new Map([["o2", "corrected"]]));
    const target = (await repo.thread("c1")).find((m) => m.msgUuid === "m1");
    expect(target?.body).toBe("corrected");
    expect(target?.edited).toBe(true);
  });

  it("deleteForMe hides a message locally without queuing an overlay", async () => {
    const repo = new MemoryMessageRepo();
    await repo.persistInboxBatch(batch([inbound(1)]), new Map([["m1", "secret"]]));
    await repo.deleteForMe("m1");
    expect((await repo.thread("c1"))[0]!.deleted).toBe(true);
    expect(await repo.pendingSends()).toEqual([]);
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

  it("searches outgoing bodies, scopes by conversation, and excludes deletes", async () => {
    const repo = new MemoryMessageRepo();
    await repo.enqueueOutgoing({ clientRef: "m1", conversationId: "c1", plaintext: "Lunch at noon?", payload: new Uint8Array(), now: 1 });
    await repo.enqueueOutgoing({ clientRef: "m2", conversationId: "c1", plaintext: "dinner plans tonight", payload: new Uint8Array(), now: 2 });
    await repo.enqueueOutgoing({ clientRef: "m3", conversationId: "c2", plaintext: "lunch meeting elsewhere", payload: new Uint8Array(), now: 3 });

    // Prefix match across conversations.
    const all = await repo.search("lun");
    expect(all.map((h) => h.msgUuid).sort()).toEqual(["m1", "m3"]);
    expect(all.some((h) => h.snippet.includes("‹"))).toBe(true);

    // Conversation scope.
    const scoped = await repo.search("lunch", { conversationId: "c2" });
    expect(scoped.map((h) => h.msgUuid)).toEqual(["m3"]);

    // A whitespace query yields nothing.
    expect(await repo.search("   ")).toEqual([]);

    // The limit caps results.
    expect(await repo.search("lunch", { limit: 1 })).toHaveLength(1);
  });

  it("filters search by sender, date, media, and hashtag (T6.05)", async () => {
    const repo = new MemoryMessageRepo();
    // Mine: text with a hashtag, recent.
    await repo.enqueueOutgoing({ clientRef: "mine1", conversationId: "c1", plaintext: "beach trip #summer plan", payload: new Uint8Array(), now: 1000 });
    // Theirs: plain text, old.
    await repo.persistInboxBatch(batch([inbound(1, { msgUuid: "theirs1" })]), new Map([["theirs1", "beach trip photos coming"]]));
    // Theirs: a MEDIA message, old.
    await repo.persistInboxBatch(batch([inbound(2, { msgUuid: "media1", kind: MsgKind.MEDIA })]), new Map([["media1", "beach sunset image"]]));

    // By user.
    expect((await repo.search("beach", { fromMe: true })).map((h) => h.msgUuid)).toEqual(["mine1"]);
    expect((await repo.search("beach", { fromMe: false })).map((h) => h.msgUuid).sort()).toEqual(["media1", "theirs1"]);
    // By file (even alongside a text term); and filter-only (no term).
    expect((await repo.search("beach", { mediaOnly: true })).map((h) => h.msgUuid)).toEqual(["media1"]);
    expect((await repo.search("", { mediaOnly: true })).map((h) => h.msgUuid)).toEqual(["media1"]);
    // By hashtag (filter-only) — only my message carries #summer.
    expect((await repo.search("", { hashtag: "summer" })).map((h) => h.msgUuid)).toEqual(["mine1"]);
    // By date — inbound createdAt is seq*10 (10, 20); mine is 1000.
    expect((await repo.search("beach", { after: 500 })).map((h) => h.msgUuid)).toEqual(["mine1"]);
    // A date/sender filter with no term/hashtag/media yields nothing (no store dump).
    expect(await repo.search("", { after: 0 })).toEqual([]);
  });

  it("does not return tombstoned messages from search", async () => {
    const repo = new MemoryMessageRepo();
    // An incoming message whose body an overlay deletes.
    await repo.persistInboxBatch(batch([inbound(1, { msgUuid: "x1" })]));
    // Give it a searchable body, then delete it.
    (await repo.thread("c1")); // no-op read
    await repo.enqueueOutgoing({ clientRef: "x1", conversationId: "c1", plaintext: "secret keyword", payload: new Uint8Array(), now: 5 });
    expect(await repo.search("keyword")).toHaveLength(1);
    await repo.persistInboxBatch(batch([inbound(2, { kind: MsgKind.OVERLAY_DELETE, overlayTarget: "x1", msgUuid: "d2" })]));
    expect(await repo.search("keyword")).toHaveLength(0);
  });

  it("pins and stars a message locally (independent, toggleable flags)", async () => {
    const repo = new MemoryMessageRepo();
    await repo.enqueueOutgoing({ clientRef: "m1", conversationId: "c1", plaintext: "hi", payload: new Uint8Array(), now: 1 });
    const read = async () => (await repo.thread("c1")).find((m) => m.msgUuid === "m1");
    expect((await read())?.pinned).toBe(false);

    await repo.setPinned("m1", true);
    await repo.setStarred("m1", true);
    expect((await read())?.pinned).toBe(true);
    expect((await read())?.starred).toBe(true);

    await repo.setPinned("m1", false);
    expect((await read())?.pinned).toBe(false);
    expect((await read())?.starred).toBe(true); // star unaffected
  });

  it("folds an inbound peer REACTION into the target's tally (T5.05b)", async () => {
    const repo = new MemoryMessageRepo();
    await repo.persistInboxBatch(batch([inbound(1)]), new Map([["m1", "hi"]]));
    await repo.persistInboxBatch(
      batch([inbound(2, { kind: MsgKind.REACTION, overlayTarget: "m1", msgUuid: "r1" })]),
      new Map([["r1", JSON.stringify({ t: "react", emoji: "👍", op: "add" })]]),
    );
    const m = (await repo.thread("c1")).find((x) => x.msgUuid === "m1");
    expect(m?.reactions).toEqual([{ emoji: "👍", count: 1, mine: false }]);
  });

  it("applies my outgoing REACTION optimistically (mine+count), no new bubble, and toggles off", async () => {
    const repo = new MemoryMessageRepo();
    await repo.enqueueOutgoing({ clientRef: "m1", conversationId: "c1", plaintext: "hi", payload: new Uint8Array(), now: 1 });
    const react = (op: "add" | "remove", ref: string) =>
      repo.enqueueOutgoing({
        clientRef: ref,
        conversationId: "c1",
        plaintext: JSON.stringify({ t: "react", emoji: "❤️", op }),
        payload: new Uint8Array(),
        now: 2,
        kind: MsgKind.REACTION,
        overlayTarget: "m1",
      });
    await react("add", "r1");
    expect((await repo.thread("c1")).find((x) => x.msgUuid === "m1")?.reactions).toEqual([{ emoji: "❤️", count: 1, mine: true }]);
    expect((await repo.thread("c1")).map((x) => x.msgUuid)).toEqual(["m1"]); // overlay is not its own bubble
    await react("remove", "r2");
    expect((await repo.thread("c1")).find((x) => x.msgUuid === "m1")?.reactions).toEqual([]); // dropped at zero
  });

  it("aggregates my + a peer's reaction on the same emoji into one count", async () => {
    const repo = new MemoryMessageRepo();
    await repo.enqueueOutgoing({ clientRef: "m1", conversationId: "c1", plaintext: "hi", payload: new Uint8Array(), now: 1 });
    await repo.enqueueOutgoing({
      clientRef: "r1",
      conversationId: "c1",
      plaintext: JSON.stringify({ t: "react", emoji: "🙏", op: "add" }),
      payload: new Uint8Array(),
      now: 2,
      kind: MsgKind.REACTION,
      overlayTarget: "m1",
    });
    await repo.persistInboxBatch(
      batch([inbound(3, { kind: MsgKind.REACTION, overlayTarget: "m1", msgUuid: "r2" })]),
      new Map([["r2", JSON.stringify({ t: "react", emoji: "🙏", op: "add" })]]),
    );
    const m = (await repo.thread("c1")).find((x) => x.msgUuid === "m1");
    expect(m?.reactions).toEqual([{ emoji: "🙏", count: 2, mine: true }]);
  });
});
