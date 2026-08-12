// MemoryMessageRepo is an in-memory MessageRepo mirroring MessageStore's
// semantics without SQL — the web shell runs it inside the dedicated DB/crypto
// worker until the OPFS SQLite-wasm store lands (web-app-architecture.md §1).
// It reuses the shared planInboxBatch merge, so inbox dedupe/overlay/watermark
// behaviour is identical to the SQLite path.

import { Cursors } from "@wa/sync-engine";
import { MsgKind, type ConversationCursor, type InboxBatch, type MsgSend } from "./frames";
import {
  planInboxBatch,
  type ChatSummary,
  type MessageRepo,
  type OutgoingDraft,
  type ThreadMessage,
} from "./db/messageStore";
import { DEFAULT_SEARCH_LIMIT, matchMemory, tokenize, type SearchHit, type SearchOptions } from "./search";

interface MsgRow {
  msgUuid: string;
  conversationId: string;
  seq: number;
  body: string;
  deleted: boolean;
  mine: boolean;
  state: string;
  pinned?: boolean;
  starred?: boolean;
  acceptedAt: number;
  createdAt: number;
}

interface ConvRow {
  id: string;
  title: string;
  lastPreview: string;
  lastSeq: number;
  updatedAt: number;
}

interface OutboxRow {
  clientRef: string;
  conversationId: string;
  payload: Uint8Array;
  createdAt: number;
}

export class MemoryMessageRepo implements MessageRepo {
  private readonly cursors = new Cursors();
  private readonly messages = new Map<string, MsgRow>(); // keyed by msgUuid (dedupe)
  private readonly convs = new Map<string, ConvRow>();
  private readonly outbox: OutboxRow[] = [];

  init(): Promise<ConversationCursor[]> {
    return Promise.resolve(this.cursorSnapshot());
  }

  cursorSnapshot(): ConversationCursor[] {
    return this.cursors.snapshot().map((c) => ({ conversationId: c.conversationId, lastSeq: c.lastSeq }));
  }

  persistInboxBatch(batch: InboxBatch): Promise<ConversationCursor[]> {
    const plan = planInboxBatch(batch, this.cursors);
    for (const ins of plan.inserts) {
      if (!this.messages.has(ins.msgUuid)) {
        this.messages.set(ins.msgUuid, {
          msgUuid: ins.msgUuid,
          conversationId: ins.conversationId,
          seq: ins.seq,
          body: "[encrypted]",
          deleted: false,
          mine: false,
          state: "received",
          acceptedAt: ins.acceptedAtMs,
          createdAt: ins.acceptedAtMs,
        });
      }
      this.touch(ins.conversationId, ins.seq, "New message", ins.acceptedAtMs);
    }
    for (const ov of plan.overlays) {
      if (ov.kind === MsgKind.OVERLAY_DELETE) {
        const m = this.messages.get(ov.targetMsgUuid);
        if (m) {
          m.deleted = true;
          m.body = "";
        }
      }
    }
    return Promise.resolve(plan.watermark);
  }

  enqueueOutgoing(d: OutgoingDraft): Promise<void> {
    this.messages.set(d.clientRef, {
      msgUuid: d.clientRef,
      conversationId: d.conversationId,
      seq: 0,
      body: d.plaintext,
      deleted: false,
      mine: true,
      state: "sending",
      acceptedAt: 0,
      createdAt: d.now,
    });
    this.outbox.push({ clientRef: d.clientRef, conversationId: d.conversationId, payload: d.payload, createdAt: d.now });
    this.touch(d.conversationId, 0, d.plaintext, d.now);
    return Promise.resolve();
  }

  markSent(clientRef: string, seq: number): Promise<void> {
    const i = this.outbox.findIndex((o) => o.clientRef === clientRef);
    if (i >= 0) this.outbox.splice(i, 1);
    const m = this.messages.get(clientRef);
    if (m) {
      m.state = "sent";
      m.seq = seq;
    }
    return Promise.resolve();
  }

  pendingSends(): Promise<MsgSend[]> {
    const sorted = [...this.outbox].sort((a, b) => a.createdAt - b.createdAt || a.clientRef.localeCompare(b.clientRef));
    // Explicit MsgSend[] so the "msg_send" discriminant stays a literal through
    // Promise.resolve (which would otherwise widen it to string).
    const sends: MsgSend[] = sorted.map((o) => ({
      t: "msg_send",
      clientRef: o.clientRef,
      msgUuid: o.clientRef,
      conversationId: o.conversationId,
      kind: MsgKind.TEXT,
      sealedEnvelope: o.payload,
    }));
    return Promise.resolve(sends);
  }

  conversations(): Promise<ChatSummary[]> {
    const list = [...this.convs.values()]
      .sort((a, b) => b.updatedAt - a.updatedAt)
      .map((c) => ({
        conversationId: c.id,
        title: c.title || c.id,
        lastPreview: c.lastPreview,
        lastSeq: c.lastSeq,
        updatedAt: c.updatedAt,
      }));
    return Promise.resolve(list);
  }

  thread(conversationId: string): Promise<ThreadMessage[]> {
    const rows = [...this.messages.values()]
      .filter((m) => m.conversationId === conversationId)
      .sort((a, b) => (a.acceptedAt || a.createdAt) - (b.acceptedAt || b.createdAt) || a.seq - b.seq)
      .map((m) => ({
        msgUuid: m.msgUuid,
        seq: m.seq,
        body: m.body,
        mine: m.mine,
        state: m.state,
        deleted: m.deleted,
        pinned: m.pinned ?? false,
        starred: m.starred ?? false,
        createdAt: m.createdAt,
      }));
    return Promise.resolve(rows);
  }

  setPinned(msgUuid: string, pinned: boolean): Promise<void> {
    const m = this.messages.get(msgUuid);
    if (m) m.pinned = pinned;
    return Promise.resolve();
  }

  setStarred(msgUuid: string, starred: boolean): Promise<void> {
    const m = this.messages.get(msgUuid);
    if (m) m.starred = starred;
    return Promise.resolve();
  }

  /** search mirrors MessageStore.search over the in-memory rows with the shared
   *  prefix-token match/score (same semantics as the FTS5 path), ranked best
   *  first and excluding tombstoned messages. */
  search(query: string, opts: SearchOptions = {}): Promise<SearchHit[]> {
    const tokens = tokenize(query);
    if (tokens.length === 0) return Promise.resolve([]);
    const limit = opts.limit ?? DEFAULT_SEARCH_LIMIT;

    const scored: { hit: SearchHit; score: number }[] = [];
    for (const m of this.messages.values()) {
      if (m.deleted) continue;
      if (opts.conversationId && m.conversationId !== opts.conversationId) continue;
      const res = matchMemory(m.body, tokens);
      if (!res.matched) continue;
      scored.push({
        score: res.score,
        hit: {
          msgUuid: m.msgUuid,
          conversationId: m.conversationId,
          conversationTitle: this.convs.get(m.conversationId)?.title || m.conversationId,
          seq: m.seq,
          body: m.body,
          snippet: res.snippet,
          mine: m.mine,
          createdAt: m.createdAt,
        },
      });
    }
    scored.sort((a, b) => b.score - a.score || b.hit.createdAt - a.hit.createdAt);
    return Promise.resolve(scored.slice(0, limit).map((s) => s.hit));
  }

  private touch(conversationId: string, seq: number, preview: string, ts: number): void {
    const cur = this.convs.get(conversationId);
    if (!cur) {
      this.convs.set(conversationId, { id: conversationId, title: "", lastPreview: preview, lastSeq: seq, updatedAt: ts });
      return;
    }
    cur.lastPreview = preview;
    cur.lastSeq = Math.max(cur.lastSeq, seq);
    cur.updatedAt = Math.max(cur.updatedAt, ts);
  }
}
