-- Conversation membership. Groups already have group_members; direct (1:1)
-- conversations need their own membership so the accept pipeline can resolve
-- recipient devices. direct_key deduplicates 1:1 conversations by user pair.

ALTER TABLE conversations ADD COLUMN direct_key text;
CREATE UNIQUE INDEX conversations_direct_key ON conversations (direct_key) WHERE direct_key IS NOT NULL;

CREATE TABLE conversation_members (
    conversation_id uuid NOT NULL REFERENCES conversations (id) ON DELETE CASCADE,
    user_id         uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    added_at        timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (conversation_id, user_id)
);
-- "which conversations is this user in" — membership checks + recipient resolution.
CREATE INDEX conversation_members_by_user ON conversation_members (user_id);
