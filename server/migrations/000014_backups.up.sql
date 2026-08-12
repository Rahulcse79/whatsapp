-- Encrypted backups (FR-SYNC-04). Client-encrypted archive (Argon2id-derived
-- key, never sent) uploaded direct-to-MinIO via presigned multipart; this table
-- is the registry only — ciphertext ref + size, never the key. One COMPLETE
-- backup per user survives (a new one replaces the old on completion).

CREATE TABLE backups (
    id           uuid PRIMARY KEY,
    user_id      uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    object_key   text NOT NULL UNIQUE,           -- MinIO key of the ciphertext archive
    size_bytes   bigint NOT NULL,
    upload_state smallint NOT NULL DEFAULT 0,     -- 0 pending | 1 complete
    handle       text,                            -- MinIO multipart uploadId (pending only)
    created_at   timestamptz NOT NULL DEFAULT now()
);

-- "the user's latest complete backup" (restore) + old-backup reclaim.
CREATE INDEX backups_by_user ON backups (user_id, upload_state, created_at DESC);
