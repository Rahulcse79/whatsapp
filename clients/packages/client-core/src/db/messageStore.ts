// The local message store: persists inbox batches, holds the optimistic outbox
// view, and reads chat-list / thread views for the UI. It composes
// @wa/sync-engine's Cursors (forward-only watermarks) and the conflict rules
// (delete-wins overlays). The pure `planInboxBatch` is unit-tested; the SQL
// application is a thin adapter over the SqliteDB seam.

import { Cursors } from "@wa/sync-engine";
import { MsgKind, type ConversationCursor, type InboxBatch, type MsgSend } from "../frames";
import type { SqliteDB, SqlValue } from "../ports";
import {
  DEFAULT_SEARCH_LIMIT,
  SNIPPET_CLOSE,
  SNIPPET_OPEN,
  toFtsQuery,
  type SearchHit,
  type SearchOptions,
} from "../search";
import { SCHEMA } from "./schema";

export interface MessageInsert {
  msgUuid: string;
  conversationId: string;
  seq: number;
  sender: string;
  kind: MsgKind;
  acceptedAtMs: number;
}

export interface OverlayApply {
  conversationId: string;
  targetMsgUuid: string;
  kind: MsgKind; // OVERLAY_EDIT | OVERLAY_DELETE
}

export interface BatchPlan {
  inserts: MessageInsert[];
  overlays: OverlayApply[];
  watermark: ConversationCursor[]; // cumulative per-conversation max seq to ClientAck
}

/** Overlay kinds reference an existing message rather than creating a new one
 *  (edit/delete/reaction/pin). Everything else is a new message. */
export function isOverlayKind(kind: MsgKind): boolean {
  return (
    kind === MsgKind.OVERLAY_EDIT ||
    kind === MsgKind.OVERLAY_DELETE ||
    kind === MsgKind.REACTION ||
    kind === MsgKind.PIN
  );
}

/**
 * planInboxBatch decides — purely — what an inbox batch does to local state:
 * which non-overlay messages are new (seq beyond the cursor; at-least-once
 * duplicates are skipped), which overlays to fold in, and the cumulative
 * watermark to ClientAck. It advances the passed Cursors as a side effect.
 */
export function planInboxBatch(batch: InboxBatch, cursors: Cursors): BatchPlan {
  const inserts: MessageInsert[] = [];
  const overlays: OverlayApply[] = [];
  const touched = new Set<string>();

  for (const it of [...batch.items].sort((a, b) => a.seq - b.seq)) {
    touched.add(it.conversationId);
    if (isOverlayKind(it.kind)) {
      // edit/delete/reaction/pin reference an existing message — they are never
      // new bubbles. Content (edit body, reaction emoji) rides the ciphertext;
      // the repo applies it after decryption. Delete needs no content.
      overlays.push({ conversationId: it.conversationId, targetMsgUuid: it.overlayTarget ?? "", kind: it.kind });
    } else if (it.seq > cursors.get(it.conversationId)) {
      inserts.push({
        msgUuid: it.msgUuid,
        conversationId: it.conversationId,
        seq: it.seq,
        sender: it.senderUserId,
        kind: it.kind,
        acceptedAtMs: it.acceptedAtMs,
      });
    }
    cursors.advance(it.conversationId, it.seq);
  }

  const watermark: ConversationCursor[] = [...touched].map((c) => ({ conversationId: c, lastSeq: cursors.get(c) }));
  return { inserts, overlays, watermark };
}

export interface ChatSummary {
  conversationId: string;
  title: string;
  lastPreview: string;
  lastSeq: number;
  updatedAt: number;
}

export interface ThreadMessage {
  msgUuid: string;
  seq: number;
  body: string;
  mine: boolean;
  state: string;
  deleted: boolean;
  pinned: boolean;
  starred: boolean;
  createdAt: number;
}

export interface OutgoingDraft {
  clientRef: string;
  conversationId: string;
  plaintext: string; // kept locally for the optimistic bubble; never sent in clear
  payload: Uint8Array; // sealed envelope actually transmitted
  now: number;
  /** Human text for the chat-list preview. Defaults to plaintext; set it when
   *  plaintext is an encoded body (e.g. a link-preview message) so the list
   *  shows the message, not its JSON. */
  listText?: string;
}

/**
 * MessageRepo is the local-store contract the UI/services depend on, independent
 * of the backing store. MessageStore implements it over the SqliteDB seam
 * (mobile: expo-sqlite; web: OPFS SQLite-wasm). MemoryMessageRepo is the
 * in-memory variant the web shell's worker uses until the OPFS store lands.
 */
export interface MessageRepo {
  init(): Promise<ConversationCursor[]>;
  cursorSnapshot(): ConversationCursor[];
  persistInboxBatch(batch: InboxBatch): Promise<ConversationCursor[]>;
  enqueueOutgoing(d: OutgoingDraft): Promise<void>;
  markSent(clientRef: string, seq: number): Promise<void>;
  pendingSends(): Promise<MsgSend[]>;
  conversations(): Promise<ChatSummary[]>;
  thread(conversationId: string): Promise<ThreadMessage[]>;
  /** Local pin (syncs to own devices in the full build). */
  setPinned(msgUuid: string, pinned: boolean): Promise<void>;
  /** Purely local star — never leaves the device. */
  setStarred(msgUuid: string, starred: boolean): Promise<void>;
  /** Full-text search over decrypted local message bodies (ADR-005). */
  search(query: string, opts?: SearchOptions): Promise<SearchHit[]>;
}

export class MessageStore implements MessageRepo {
  constructor(
    private readonly db: SqliteDB,
    private readonly cursors: Cursors,
  ) {}

  /** init creates the schema, hydrates cursors from disk, and returns the
   *  cursor snapshot (seeds a caller's Hello.last_cursors mirror). */
  async init(): Promise<ConversationCursor[]> {
    await this.db.exec(SCHEMA);
    const rows = await this.db.all("SELECT conversation_id, last_seq FROM cursors");
    this.cursors.load(rows.map((r) => ({ conversationId: String(r.conversation_id), lastSeq: Number(r.last_seq) })));
    return this.cursorSnapshot();
  }

  /** cursorSnapshot feeds Hello.last_cursors on connect. */
  cursorSnapshot(): ConversationCursor[] {
    return this.cursors.snapshot().map((c) => ({ conversationId: c.conversationId, lastSeq: c.lastSeq }));
  }

  /** persistInboxBatch durably applies a batch and returns the ClientAck watermark. */
  async persistInboxBatch(batch: InboxBatch): Promise<ConversationCursor[]> {
    const plan = planInboxBatch(batch, this.cursors);

    for (const ins of plan.inserts) {
      await this.db.run(
        "INSERT OR IGNORE INTO messages(msg_uuid, conversation_id, seq, sender, kind, body, deleted, mine, state, accepted_at, created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)",
        [ins.msgUuid, ins.conversationId, ins.seq, ins.sender, ins.kind, "[encrypted]", 0, 0, "received", ins.acceptedAtMs, ins.acceptedAtMs],
      );
      await this.touchConversation(ins.conversationId, ins.seq, "New message", ins.acceptedAtMs);
    }

    for (const ov of plan.overlays) {
      if (ov.kind === MsgKind.OVERLAY_DELETE) {
        // Delete is terminal (delete-wins, @wa/sync-engine conflict rules).
        await this.db.run("UPDATE messages SET deleted = 1, body = '' WHERE msg_uuid = ?", [ov.targetMsgUuid]);
      }
      // OVERLAY_EDIT rewrites the plaintext body — applied post-decryption (T0.20/T0.21).
    }

    for (const w of plan.watermark) {
      await this.db.run(
        "INSERT INTO cursors(conversation_id, last_seq) VALUES(?,?) ON CONFLICT(conversation_id) DO UPDATE SET last_seq = excluded.last_seq",
        [w.conversationId, w.lastSeq],
      );
    }
    return plan.watermark;
  }

  /** enqueueOutgoing writes the optimistic bubble + the durable outbox row in
   *  one logical step (persist-before-send). */
  async enqueueOutgoing(d: OutgoingDraft): Promise<void> {
    await this.db.run(
      "INSERT OR REPLACE INTO messages(msg_uuid, conversation_id, seq, sender, kind, body, deleted, mine, state, accepted_at, created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)",
      [d.clientRef, d.conversationId, 0, "", MsgKind.TEXT, d.plaintext, 0, 1, "sending", 0, d.now],
    );
    await this.db.run(
      "INSERT OR REPLACE INTO outbox(client_ref, conversation_id, payload, attempts, created_at) VALUES(?,?,?,?,?)",
      [d.clientRef, d.conversationId, d.payload, 0, d.now],
    );
    await this.touchConversation(d.conversationId, 0, d.listText ?? d.plaintext, d.now);
  }

  /** markSent clears the outbox row and flips the bubble to "sent" on MsgAck. */
  async markSent(clientRef: string, seq: number): Promise<void> {
    await this.db.run("DELETE FROM outbox WHERE client_ref = ?", [clientRef]);
    await this.db.run("UPDATE messages SET state = 'sent', seq = ? WHERE msg_uuid = ?", [seq, clientRef]);
  }

  /** pendingSends turns the durable outbox into MsgSend frames for (re)transmit. */
  async pendingSends(): Promise<MsgSend[]> {
    const rows = await this.db.all(
      "SELECT client_ref, conversation_id, payload FROM outbox ORDER BY created_at ASC, client_ref ASC",
    );
    return rows.map((r) => ({
      t: "msg_send",
      clientRef: String(r.client_ref),
      msgUuid: String(r.client_ref),
      conversationId: String(r.conversation_id),
      kind: MsgKind.TEXT,
      sealedEnvelope: r.payload instanceof Uint8Array ? r.payload : new Uint8Array(),
    }));
  }

  async conversations(): Promise<ChatSummary[]> {
    const rows = await this.db.all(
      "SELECT id, title, last_preview, last_seq, updated_at FROM conversations ORDER BY updated_at DESC",
    );
    return rows.map((r) => ({
      conversationId: String(r.id),
      title: String(r.title) || String(r.id),
      lastPreview: String(r.last_preview),
      lastSeq: Number(r.last_seq),
      updatedAt: Number(r.updated_at),
    }));
  }

  async thread(conversationId: string): Promise<ThreadMessage[]> {
    const rows = await this.db.all(
      "SELECT msg_uuid, seq, body, deleted, mine, state, pinned, starred, accepted_at, created_at FROM messages WHERE conversation_id = ? ORDER BY (CASE WHEN accepted_at > 0 THEN accepted_at ELSE created_at END) ASC, seq ASC",
      [conversationId],
    );
    return rows.map((r) => ({
      msgUuid: String(r.msg_uuid),
      seq: Number(r.seq),
      body: String(r.body),
      deleted: Number(r.deleted) === 1,
      mine: Number(r.mine) === 1,
      state: String(r.state),
      pinned: Number(r.pinned) === 1,
      starred: Number(r.starred) === 1,
      createdAt: Number(r.created_at),
    }));
  }

  async setPinned(msgUuid: string, pinned: boolean): Promise<void> {
    await this.db.run("UPDATE messages SET pinned = ? WHERE msg_uuid = ?", [pinned ? 1 : 0, msgUuid]);
  }

  async setStarred(msgUuid: string, starred: boolean): Promise<void> {
    await this.db.run("UPDATE messages SET starred = ? WHERE msg_uuid = ?", [starred ? 1 : 0, msgUuid]);
  }

  /**
   * search runs the sanitised query against the FTS5 index, ranks by bm25, and
   * returns a highlighted snippet per hit. Deleted (tombstoned) messages are
   * excluded. The SNIPPET markers are module constants (never user input), so
   * inlining them in the snippet() call is injection-safe; the MATCH expression
   * and all filters are bound parameters.
   */
  async search(query: string, opts: SearchOptions = {}): Promise<SearchHit[]> {
    const match = toFtsQuery(query);
    if (!match) return [];

    const params: SqlValue[] = [match];
    let convClause = "";
    if (opts.conversationId) {
      convClause = " AND m.conversation_id = ?";
      params.push(opts.conversationId);
    }
    params.push(opts.limit ?? DEFAULT_SEARCH_LIMIT);

    const rows = await this.db.all(
      `SELECT m.msg_uuid, m.conversation_id, m.seq, m.body, m.mine, m.created_at,
              snippet(messages_fts, 0, '${SNIPPET_OPEN}', '${SNIPPET_CLOSE}', '…', 12) AS snippet,
              COALESCE(c.title, m.conversation_id) AS title
         FROM messages_fts
         JOIN messages m ON m.rowid = messages_fts.rowid
         LEFT JOIN conversations c ON c.id = m.conversation_id
        WHERE messages_fts MATCH ? AND m.deleted = 0${convClause}
        ORDER BY bm25(messages_fts)
        LIMIT ?`,
      params,
    );
    return rows.map((r) => ({
      msgUuid: String(r.msg_uuid),
      conversationId: String(r.conversation_id),
      conversationTitle: String(r.title),
      seq: Number(r.seq),
      body: String(r.body),
      snippet: String(r.snippet),
      mine: Number(r.mine) === 1,
      createdAt: Number(r.created_at),
    }));
  }

  private async touchConversation(conversationId: string, seq: number, preview: string, ts: number): Promise<void> {
    await this.db.run(
      "INSERT INTO conversations(id, title, last_preview, last_seq, updated_at) VALUES(?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET last_preview = excluded.last_preview, last_seq = MAX(conversations.last_seq, excluded.last_seq), updated_at = MAX(conversations.updated_at, excluded.updated_at)",
      [conversationId, "", preview, seq, ts],
    );
  }
}
