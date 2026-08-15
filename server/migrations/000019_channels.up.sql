-- Channels (T7.01): broadcast / one-to-many publishing. Unlike the E2EE
-- messaging plane, a channel post's content is server-visible: a channel
-- broadcasts to an unbounded follower set, so sender-key E2EE to every follower
-- is infeasible, and public/verified channels are public media by design.
-- Access is controlled by kind (public vs private) + membership, not by content
-- encryption. Private channels restrict WHO may follow/read, not the ciphertext.

CREATE TABLE channels (
    id          uuid PRIMARY KEY,
    owner_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    handle      text NOT NULL,                        -- public @handle
    name        text NOT NULL,
    description text NOT NULL DEFAULT '',
    kind        smallint NOT NULL DEFAULT 0,          -- 0 public | 1 private
    verified    boolean NOT NULL DEFAULT false,       -- set by the admin plane, not self-serve
    created_at  timestamptz NOT NULL DEFAULT now(),
    deleted_at  timestamptz
);
-- Case-insensitive handle uniqueness among live channels.
CREATE UNIQUE INDEX channels_handle ON channels (lower(handle)) WHERE deleted_at IS NULL;
CREATE INDEX channels_by_owner ON channels (owner_id);
-- Discovery/search over public channels (trigram on name + handle).
CREATE INDEX channels_name_trgm ON channels USING gin (name gin_trgm_ops) WHERE kind = 0 AND deleted_at IS NULL;

-- channel_members unifies followers and admin roles:
--   role 0 follower | 1 admin | 2 owner.
-- Following inserts a role-0 row; promotion bumps the role. Follower count is
-- count(*), admins are role >= 1.
CREATE TABLE channel_members (
    channel_id uuid NOT NULL REFERENCES channels (id) ON DELETE CASCADE,
    user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    role       smallint NOT NULL DEFAULT 0,
    joined_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (channel_id, user_id)
);
-- "which channels does this user follow" — the follower's home feed source.
CREATE INDEX channel_members_by_user ON channel_members (user_id);

CREATE TABLE channel_posts (
    id         uuid PRIMARY KEY,
    channel_id uuid NOT NULL REFERENCES channels (id) ON DELETE CASCADE,
    author_id  uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    body       text NOT NULL,
    media_ref  uuid,                                  -- optional media_objects id
    publish_at timestamptz NOT NULL DEFAULT now(),    -- future = scheduled
    published  boolean NOT NULL DEFAULT true,         -- false until publish_at passes
    created_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz
);
-- Channel feed: newest published posts first.
CREATE INDEX channel_posts_feed ON channel_posts (channel_id, publish_at DESC) WHERE deleted_at IS NULL AND published;
-- Scheduler sweep: due, not-yet-published posts.
CREATE INDEX channel_posts_scheduled ON channel_posts (publish_at) WHERE NOT published AND deleted_at IS NULL;

CREATE TABLE channel_post_reactions (
    post_id uuid NOT NULL REFERENCES channel_posts (id) ON DELETE CASCADE,
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    emoji   text NOT NULL,
    PRIMARY KEY (post_id, user_id, emoji)
);

CREATE TABLE channel_comments (
    id         uuid PRIMARY KEY,
    post_id    uuid NOT NULL REFERENCES channel_posts (id) ON DELETE CASCADE,
    author_id  uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    body       text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz
);
CREATE INDEX channel_comments_by_post ON channel_comments (post_id, created_at) WHERE deleted_at IS NULL;
