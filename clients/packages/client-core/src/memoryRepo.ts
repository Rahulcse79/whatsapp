// MemoryMessageRepo is an in-memory MessageRepo mirroring MessageStore's
// semantics without SQL — the web shell runs it inside the dedicated DB/crypto
// worker until the OPFS SQLite-wasm store lands (web-app-architecture.md §1).
// It reuses the shared planInboxBatch merge, so inbox dedupe/overlay/watermark
// behaviour is identical to the SQLite path.

import { Cursors } from "@wa/sync-engine";
import { MsgKind, type ConversationCursor, type InboxBatch, type MsgSend, type ReceiptKind } from "./frames";
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
  edited?: boolean;
  mine: boolean;
  state: string;
  pinned?: boolean;
  starred?: boolean;
  reactions?: Map<string, { count: number; mine: boolean }>; // emoji → tally
  acceptedAt: number;
  createdAt: number;
}

/** applyReaction folds one reaction overlay into a message's tally. `mine`
 *  distinguishes my own reaction (tracked so it toggles + highlights) from a
 *  peer's (a bare count). Removing to zero drops the emoji. */
function applyReaction(row: MsgRow, emoji: string, op: "add" | "remove", mine: boolean): void {
  if (!row.reactions) row.reactions = new Map();
  const cur = row.reactions.get(emoji) ?? { count: 0, mine: false };
  if (op === "add") {
    if (mine) {
      if (!cur.mine) { cur.mine = true; cur.count += 1; } // my re-add is idempotent
    } else {
      cur.count += 1;
    }
  } else {
    if (mine) {
      if (cur.mine) { cur.mine = false; cur.count = Math.max(0, cur.count - 1); }
    } else {
      cur.count = Math.max(0, cur.count - 1);
    }
  }
  if (cur.count <= 0) row.reactions.delete(emoji);
  else row.reactions.set(emoji, cur);
}

/** parseReactionBody reads a decrypted REACTION overlay body ({t:"react",emoji,op}).
 *  Kept local so client-core stays free of @wa/media-pipeline (which owns the
 *  canonical encoder used on the send/seal side). */
function parseReactionBody(body: string | undefined): { emoji: string; op: "add" | "remove" } | null {
  if (!body || body.charAt(0) !== "{") return null;
  try {
    const o = JSON.parse(body) as Record<string, unknown>;
    if (o.t !== "react" || typeof o.emoji !== "string") return null;
    return { emoji: o.emoji, op: o.op === "remove" ? "remove" : "add" };
  } catch {
    return null;
  }
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
  kind: MsgKind;
  overlayTarget?: string;
}

// Monotonic message-state ordering so a bubble's ticks only ever advance
// (sending → sent → delivered → read). Inbound "received" is 0 (never a tick).
const STATE_RANK: Record<string, number> = { sending: 0, received: 0, sent: 1, delivered: 2, read: 3 };
function stateRank(s: string): number {
  return STATE_RANK[s] ?? 0;
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

  // bodies (optional) maps msgUuid → decrypted plaintext; the caller (the DB/
  // crypto worker) opens each inbound ciphertext and passes the result so the
  // received bubble shows real text instead of the "[encrypted]" placeholder.
  persistInboxBatch(batch: InboxBatch, bodies?: Map<string, string>): Promise<ConversationCursor[]> {
    const plan = planInboxBatch(batch, this.cursors);
    for (const ins of plan.inserts) {
      const body = bodies?.get(ins.msgUuid) ?? "[encrypted]";
      if (!this.messages.has(ins.msgUuid)) {
        this.messages.set(ins.msgUuid, {
          msgUuid: ins.msgUuid,
          conversationId: ins.conversationId,
          seq: ins.seq,
          body,
          deleted: false,
          mine: false,
          state: "received",
          acceptedAt: ins.acceptedAtMs,
          createdAt: ins.acceptedAtMs,
        });
      }
      this.touch(ins.conversationId, ins.seq, body === "[encrypted]" ? "New message" : body, ins.acceptedAtMs);
    }
    for (const ov of plan.overlays) {
      const m = this.messages.get(ov.targetMsgUuid);
      if (!m) continue;
      if (ov.kind === MsgKind.OVERLAY_DELETE) {
        m.deleted = true;
        m.body = "";
      } else if (ov.kind === MsgKind.OVERLAY_EDIT) {
        const newBody = bodies?.get(ov.msgUuid);
        if (newBody !== undefined && !m.deleted) {
          m.body = newBody;
          m.edited = true;
        }
      } else if (ov.kind === MsgKind.REACTION && !m.deleted) {
        const r = parseReactionBody(bodies?.get(ov.msgUuid)); // inbound → peer reaction
        if (r) applyReaction(m, r.emoji, r.op, false);
      }
    }
    return Promise.resolve(plan.watermark);
  }

  enqueueOutgoing(d: OutgoingDraft): Promise<void> {
    const kind = d.kind ?? MsgKind.TEXT;

    // Overlay sends (edit/delete/react) target an existing message — apply
    // optimistically to the local bubble, and queue the overlay; never a new bubble.
    if (kind === MsgKind.OVERLAY_EDIT || kind === MsgKind.OVERLAY_DELETE || kind === MsgKind.REACTION) {
      const target = this.messages.get(d.overlayTarget ?? "");
      if (target && !target.deleted) {
        if (kind === MsgKind.OVERLAY_DELETE) {
          target.deleted = true;
          target.body = "";
        } else if (kind === MsgKind.OVERLAY_EDIT) {
          target.body = d.plaintext;
          target.edited = true;
        } else {
          const r = parseReactionBody(d.plaintext); // my own reaction
          if (r) applyReaction(target, r.emoji, r.op, true);
        }
      }
      this.outbox.push({
        clientRef: d.clientRef,
        conversationId: d.conversationId,
        payload: d.payload,
        createdAt: d.now,
        kind,
        overlayTarget: d.overlayTarget,
      });
      return Promise.resolve();
    }

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
    this.outbox.push({ clientRef: d.clientRef, conversationId: d.conversationId, payload: d.payload, createdAt: d.now, kind });
    this.touch(d.conversationId, 0, d.listText ?? d.plaintext, d.now);
    return Promise.resolve();
  }

  deleteForMe(msgUuid: string): Promise<void> {
    const m = this.messages.get(msgUuid);
    if (m) {
      m.deleted = true;
      m.body = "";
    }
    return Promise.resolve();
  }

  markSent(clientRef: string, seq: number): Promise<void> {
    const i = this.outbox.findIndex((o) => o.clientRef === clientRef);
    if (i >= 0) this.outbox.splice(i, 1);
    const m = this.messages.get(clientRef);
    if (m) {
      if (stateRank(m.state) < stateRank("sent")) m.state = "sent"; // don't undo a receipt
      m.seq = seq;
    }
    return Promise.resolve();
  }

  /** markReceipt upgrades my sent messages in a conversation (seq ≤ upToSeq) to
   *  "delivered" or "read" — monotonic, never downgrades. Drives ✓✓ / read
   *  ticks from a peer's relayed watermark. */
  markReceipt(conversationId: string, kind: ReceiptKind, upToSeq: number): Promise<void> {
    const target = kind === "READ" ? "read" : "delivered";
    for (const m of this.messages.values()) {
      if (!m.mine || m.conversationId !== conversationId) continue;
      if (m.seq <= 0 || m.seq > upToSeq) continue;
      if (stateRank(m.state) < stateRank(target)) m.state = target;
    }
    return Promise.resolve();
  }

  pendingSends(): Promise<MsgSend[]> {
    const sorted = [...this.outbox].sort((a, b) => a.createdAt - b.createdAt || a.clientRef.localeCompare(b.clientRef));
    // Explicit MsgSend[] so the "msg_send" discriminant stays a literal through
    // Promise.resolve (which would otherwise widen it to string).
    const sends: MsgSend[] = sorted.map((o) => {
      const s: MsgSend = {
        t: "msg_send",
        clientRef: o.clientRef,
        msgUuid: o.clientRef,
        conversationId: o.conversationId,
        kind: o.kind,
        sealedEnvelope: o.payload,
      };
      if (o.overlayTarget) s.overlayTarget = o.overlayTarget;
      return s;
    });
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
        edited: m.edited ?? false,
        pinned: m.pinned ?? false,
        starred: m.starred ?? false,
        reactions: m.reactions
          ? [...m.reactions.entries()].map(([emoji, r]) => ({ emoji, count: r.count, mine: r.mine }))
          : [],
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
