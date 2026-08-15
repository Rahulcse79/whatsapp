-- Polls (T6.02): lifecycle metadata + votes by option INDEX. Poll content
-- (question, option texts) is E2EE and never stored here — the client seals it in
-- the message body. The server holds only the option COUNT (to validate vote
-- indices), open/closed state, and per-index votes (a metadata-only compromise).

CREATE TABLE polls (
    id              uuid PRIMARY KEY,
    conversation_id uuid NOT NULL,
    creator_id      uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    option_count    smallint NOT NULL,
    multi           boolean NOT NULL DEFAULT false,
    closed          boolean NOT NULL DEFAULT false,
    closes_at       timestamptz,                 -- optional auto-close deadline
    created_at      timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX polls_by_conversation ON polls (conversation_id);

CREATE TABLE poll_votes (
    poll_id      uuid NOT NULL REFERENCES polls (id) ON DELETE CASCADE,
    voter_id     uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    option_index smallint NOT NULL,
    -- One row per (voter, option): re-voting deletes+reinserts. Uniqueness makes
    -- count(*) per index a distinct-voter tally.
    PRIMARY KEY (poll_id, voter_id, option_index)
);
