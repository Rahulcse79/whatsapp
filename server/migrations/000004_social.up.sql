-- Groups, membership, invites, contacts, blocks.
-- Contract: Docs/03-database/database-design.md §2 "groups"/"contacts".

CREATE TABLE groups (
    id         uuid PRIMARY KEY,
    name       text NOT NULL,
    description text,
    avatar_ref uuid REFERENCES media_objects (id) ON DELETE SET NULL,
    settings   jsonb NOT NULL DEFAULT '{}',        -- {who_can_post, who_can_edit_info, announcements}
    version    bigint NOT NULL DEFAULT 0,          -- bumped on every membership/settings change (Sender-Key rotation signal)
    created_by uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

-- Group-name metadata search (ADR-005).
CREATE INDEX groups_name_trgm ON groups USING gin (name gin_trgm_ops);

CREATE TABLE group_members (
    group_id  uuid NOT NULL REFERENCES groups (id) ON DELETE CASCADE,
    user_id   uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    role      smallint NOT NULL DEFAULT 0,         -- 0 member | 1 admin | 2 owner
    joined_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (group_id, user_id)
);
-- "my groups" + fan-out membership loads.
CREATE INDEX members_by_user ON group_members (user_id);

CREATE TABLE invite_links (
    token      text PRIMARY KEY,                   -- capability URL token
    group_id   uuid NOT NULL REFERENCES groups (id) ON DELETE CASCADE,
    created_by uuid REFERENCES users (id) ON DELETE SET NULL,
    expires_at timestamptz,
    revoked_at timestamptz,
    max_uses   integer,
    uses       integer NOT NULL DEFAULT 0
);

CREATE TABLE contacts (
    owner_id           uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    contact_phone_hash bytea NOT NULL,              -- peppered HMAC — plaintext address books never reach the server
    matched_user_id    uuid REFERENCES users (id) ON DELETE SET NULL,
    favorite           boolean NOT NULL DEFAULT false,
    PRIMARY KEY (owner_id, contact_phone_hash)
);

CREATE TABLE blocks (
    blocker_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    blocked_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (blocker_id, blocked_id)
);
