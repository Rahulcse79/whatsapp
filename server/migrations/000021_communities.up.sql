-- Communities (T8.01): a roof over many chat groups + an announcement group,
-- with their own membership, roles, shared calendar, and public discovery. Group
-- messages stay E2EE in the groups context; this stores only the structure.

CREATE TABLE communities (
    id                    uuid PRIMARY KEY,
    name                  text NOT NULL,
    description           text NOT NULL DEFAULT '',
    kind                  smallint NOT NULL DEFAULT 0,      -- 0 public | 1 private
    owner_id              uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    announcement_group_id uuid NOT NULL,                    -- a groups.id (lifecycle independent)
    created_at            timestamptz NOT NULL DEFAULT now()
);

-- Discovery: public communities by name (trigram, mirrors channels).
CREATE INDEX communities_name_trgm ON communities USING gin (name gin_trgm_ops) WHERE kind = 0;
CREATE INDEX communities_public ON communities (created_at DESC) WHERE kind = 0;

CREATE TABLE community_members (
    community_id uuid NOT NULL REFERENCES communities (id) ON DELETE CASCADE,
    user_id      uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    role         smallint NOT NULL DEFAULT 0,               -- 0 member | 1 admin | 2 owner
    joined_at    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (community_id, user_id)
);
CREATE INDEX community_members_by_user ON community_members (user_id);

CREATE TABLE community_groups (
    community_id uuid NOT NULL REFERENCES communities (id) ON DELETE CASCADE,
    group_id     uuid NOT NULL,
    added_at     timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (community_id, group_id)
);

CREATE TABLE community_events (
    id           uuid PRIMARY KEY,
    community_id uuid NOT NULL REFERENCES communities (id) ON DELETE CASCADE,
    title        text NOT NULL,
    description  text NOT NULL DEFAULT '',
    starts_at    timestamptz NOT NULL,
    created_by   uuid NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX community_events_by_time ON community_events (community_id, starts_at);
