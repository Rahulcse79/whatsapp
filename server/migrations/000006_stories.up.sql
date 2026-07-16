-- Stories: ciphertext refs + metadata; 24 h hard expiry (HLD §12).

CREATE TABLE stories (
    id                uuid PRIMARY KEY,
    author_id         uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    media_ref         uuid REFERENCES media_objects (id) ON DELETE SET NULL,
    audience_snapshot uuid[] NOT NULL,             -- eligible user ids frozen at post time
    expires_at        timestamptz NOT NULL,        -- post + 24 h, hard delete job + MinIO ILM backstop
    created_at        timestamptz NOT NULL DEFAULT now()
);

-- Feed query: "stories whose audience includes me".
CREATE INDEX stories_audience ON stories USING gin (audience_snapshot);
CREATE INDEX stories_expiry ON stories (expires_at);

CREATE TABLE story_views (
    story_id  uuid NOT NULL REFERENCES stories (id) ON DELETE CASCADE,
    viewer_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    viewed_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (story_id, viewer_id)
);
