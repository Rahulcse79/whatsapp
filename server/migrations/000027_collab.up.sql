-- Collaboration (T12.01): shared notes (versioned + revision history), task
-- lists, comments, an approval workflow, and an activity timeline, scoped to a
-- conversation. Access is gated on conversation_members. Content here is
-- server-visible collaboration state (not E2EE messages) — notes/tasks the
-- members deliberately share into a workspace.

CREATE TABLE collab_notes (
    id              uuid PRIMARY KEY,
    conversation_id uuid NOT NULL REFERENCES conversations (id) ON DELETE CASCADE,
    title           text NOT NULL,
    body            text NOT NULL DEFAULT '',
    version         integer NOT NULL DEFAULT 1,
    approval_state  smallint NOT NULL DEFAULT 0, -- 0 none | 1 pending | 2 approved | 3 rejected
    approver        uuid,
    decided_at      timestamptz,
    created_by      uuid NOT NULL,
    updated_by      uuid NOT NULL,
    updated_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX collab_notes_by_conversation ON collab_notes (conversation_id, updated_at DESC);

CREATE TABLE collab_note_revisions (
    id         uuid PRIMARY KEY,
    note_id    uuid NOT NULL REFERENCES collab_notes (id) ON DELETE CASCADE,
    version    integer NOT NULL,
    title      text NOT NULL,
    body       text NOT NULL,
    author     uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (note_id, version)
);

CREATE TABLE collab_tasks (
    id              uuid PRIMARY KEY,
    conversation_id uuid NOT NULL REFERENCES conversations (id) ON DELETE CASCADE,
    title           text NOT NULL,
    done            boolean NOT NULL DEFAULT false,
    assignee        uuid,
    created_by      uuid NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX collab_tasks_by_conversation ON collab_tasks (conversation_id, done, created_at);

CREATE TABLE collab_comments (
    id         uuid PRIMARY KEY,
    note_id    uuid NOT NULL REFERENCES collab_notes (id) ON DELETE CASCADE,
    author     uuid NOT NULL,
    body       text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX collab_comments_by_note ON collab_comments (note_id, created_at);

CREATE TABLE collab_activity (
    id              uuid PRIMARY KEY,
    conversation_id uuid NOT NULL REFERENCES conversations (id) ON DELETE CASCADE,
    actor           uuid NOT NULL,
    kind            text NOT NULL,
    summary         text NOT NULL,
    at              timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX collab_activity_by_conversation ON collab_activity (conversation_id, at DESC);
