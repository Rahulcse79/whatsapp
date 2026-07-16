-- The messaging core: conversations + the ciphertext relay buffer.
-- THE hot table. Rows are deleted on device ACK; 30-day TTL is the backstop.
-- Contract: Docs/03-database/database-design.md §2–4; deviations in
-- server/migrations/README.md.

CREATE TABLE conversations (
    id         uuid PRIMARY KEY,
    kind       smallint NOT NULL,                  -- 0 direct | 1 group
    group_id   uuid REFERENCES groups (id) ON DELETE CASCADE,
    seq        bigint NOT NULL DEFAULT 0,          -- per-conversation monotonic sequence (DS&A §2)
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX conversations_by_group ON conversations (group_id) WHERE group_id IS NOT NULL;

CREATE TABLE message_inbox (
    recipient_device_id uuid NOT NULL,
    conversation_id     uuid NOT NULL,
    seq                 bigint NOT NULL,
    msg_uuid            uuid NOT NULL,             -- client UUIDv7 (idempotency key)
    sender_device_id    uuid NOT NULL,
    kind                smallint NOT NULL,         -- MsgKind enum (proto)
    overlay_target      uuid,                      -- original msg for edit/delete/react/pin
    ciphertext          bytea NOT NULL,            -- sealed envelope; server-opaque forever
    accepted_at         timestamptz NOT NULL DEFAULT now(),
    expires_at          timestamptz NOT NULL,      -- accepted_at + 30 d
    PRIMARY KEY (recipient_device_id, conversation_id, seq, msg_uuid)
) PARTITION BY HASH (recipient_device_id);

-- No FKs on this table by design: partition locality + write volume; the
-- accept path owns integrity (database-design.md §6).

DO $$
BEGIN
    FOR i IN 0..15 LOOP
        EXECUTE format(
            'CREATE TABLE message_inbox_p%s PARTITION OF message_inbox
             FOR VALUES WITH (MODULUS 16, REMAINDER %s)', i, i);
    END LOOP;
END $$;

-- TTL sweeper scan. Replay/resume uses the primary key.
CREATE INDEX inbox_expiry ON message_inbox (expires_at);
