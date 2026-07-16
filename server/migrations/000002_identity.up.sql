-- Identity: users, devices, Signal prekey distribution (public material only).
-- Contract: Docs/03-database/database-design.md §2 "identity".

CREATE TABLE users (
    id           uuid PRIMARY KEY,                 -- UUIDv7
    phone_hash   bytea NOT NULL UNIQUE,            -- HMAC(pepper, E.164); pepper lives in secrets, never here
    phone_enc    bytea,                            -- AES-GCM, for SMS sending only; NULL in offline profile
    username     citext UNIQUE,
    display_name text,
    about        text,
    avatar_ref   uuid,                             -- FK added in 000003 (media_objects doesn't exist yet)
    privacy      jsonb NOT NULL DEFAULT '{}',      -- {last_seen,avatar,about,read_receipts}: everyone|contacts|nobody
    pin_hash     text,                             -- Argon2id 2FA registration PIN
    status       smallint NOT NULL DEFAULT 0,      -- 0 active | 1 suspended | 2 deleted (tombstone)
    created_at   timestamptz NOT NULL DEFAULT now(),
    deleted_at   timestamptz
);

-- Fuzzy username search (server-side metadata search — ADR-005).
CREATE INDEX users_username_trgm ON users USING gin ((username::text) gin_trgm_ops);

CREATE TABLE devices (
    id             uuid PRIMARY KEY,
    user_id        uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    is_primary     boolean NOT NULL,
    platform       smallint NOT NULL,              -- 0 ios | 1 android | 2 web
    device_name    text,
    identity_key   bytea NOT NULL,                 -- public Signal identity key
    cert           bytea NOT NULL,                 -- signed by the user's primary identity key (e2ee-design §5)
    registered_at  timestamptz NOT NULL DEFAULT now(),
    last_active_at timestamptz,
    revoked_at     timestamptz
);
-- Device-count cap (1 primary + ≤4 linked) is enforced in the registration
-- transaction; this index enforces the single-primary invariant.
CREATE UNIQUE INDEX one_primary_per_user ON devices (user_id) WHERE is_primary AND revoked_at IS NULL;
CREATE INDEX devices_by_user ON devices (user_id);

CREATE TABLE signed_prekeys (
    device_id  uuid NOT NULL REFERENCES devices (id) ON DELETE CASCADE,
    key_id     integer NOT NULL,
    pubkey     bytea NOT NULL,
    signature  bytea NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (device_id, key_id)
);

CREATE TABLE prekeys (                             -- one-time prekeys, consumed on session setup
    device_id   uuid NOT NULL REFERENCES devices (id) ON DELETE CASCADE,
    key_id      integer NOT NULL,
    pubkey      bytea NOT NULL,
    consumed_at timestamptz,
    PRIMARY KEY (device_id, key_id)
);
-- Bundle-fetch hot path: grab an unconsumed prekey per device.
CREATE INDEX prekeys_available ON prekeys (device_id) WHERE consumed_at IS NULL;

CREATE TABLE otp_attempts (
    phone_hash bytea NOT NULL,
    at         timestamptz NOT NULL DEFAULT now(),
    success    boolean NOT NULL,
    ip         inet,
    PRIMARY KEY (phone_hash, at)
);
-- Rows are swept by a 10-minute TTL job; long-window counters live in Valkey.
