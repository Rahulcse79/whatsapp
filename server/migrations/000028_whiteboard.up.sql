-- Collaborative whiteboard (T12.02): the board is an append-only CRDT op-log per
-- conversation (grow-only stroke set + erase tombstones + clear barrier). The
-- client owns merge/render; the server stores ops under a monotonic seq and
-- serves incremental sync. Access is gated on conversation_members. File
-- collaboration reuses the same op-log substrate.

CREATE TABLE board_ops (
    conversation_id uuid NOT NULL REFERENCES conversations (id) ON DELETE CASCADE,
    op_id           text NOT NULL,          -- client-generated op id (idempotency)
    author          uuid NOT NULL,
    seq             bigint NOT NULL,         -- Lamport clock (client-assigned)
    kind            text NOT NULL,           -- stroke | erase | clear
    data            jsonb NOT NULL,          -- opaque op payload the client CRDT reads
    created_at      timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (conversation_id, op_id)
);
-- Incremental sync scans by (conversation, seq).
CREATE INDEX board_ops_sync ON board_ops (conversation_id, seq);
