-- Media object registry. Blobs live in MinIO as ciphertext; this table is
-- refcounts + lifecycle (Docs/05-services/media-svc-lld.md).

CREATE TABLE media_objects (
    id               uuid PRIMARY KEY,
    object_key       text NOT NULL UNIQUE,          -- MinIO key
    size_bytes       bigint NOT NULL,
    content_hash     bytea NOT NULL,
    uploader_user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    refcount         integer NOT NULL DEFAULT 0,
    upload_state     smallint NOT NULL DEFAULT 0,   -- 0 pending | 1 complete | 2 gc_candidate
    created_at       timestamptz NOT NULL DEFAULT now(),
    expires_at       timestamptz                    -- last_ref + 30 d
);

-- GC sweep: unreferenced + expired (media-svc GC job, hourly).
CREATE INDEX media_gc ON media_objects (expires_at) WHERE refcount = 0;

-- Now that media_objects exists, close the forward reference from users.
ALTER TABLE users
    ADD CONSTRAINT users_avatar_ref_fkey
    FOREIGN KEY (avatar_ref) REFERENCES media_objects (id) ON DELETE SET NULL;
