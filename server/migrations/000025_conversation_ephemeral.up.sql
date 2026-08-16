-- Disappearing messages (T10.01): a coarse per-conversation timer the server
-- keeps so a fresh device can learn the setting and the relay buffer can be
-- purged early. The authoritative timer is E2EE-propagated between clients (a
-- sealed control message); the server never sees message content.

CREATE TABLE conversation_ephemeral (
    conversation_id uuid PRIMARY KEY REFERENCES conversations (id) ON DELETE CASCADE,
    ttl_seconds     integer NOT NULL DEFAULT 0, -- 0 = off
    updated_by      uuid NOT NULL,
    updated_at      timestamptz NOT NULL DEFAULT now()
);
