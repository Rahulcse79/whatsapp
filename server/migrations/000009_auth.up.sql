-- Auth runtime state: OTP challenges (survive pod death — core-api-lld §4)
-- and refresh-token sessions with rotation + reuse detection
-- (security-architecture §2).

CREATE TABLE otp_challenges (
    id          uuid PRIMARY KEY,
    phone_hash  bytea NOT NULL,                 -- HMAC(pepper, handle); plaintext handle never stored
    code_hash   bytea NOT NULL,                 -- HMAC-SHA256(salt, code); codes are never stored
    salt        bytea NOT NULL,
    channel     smallint NOT NULL,              -- 0 sms | 1 email | 2 mock(dev)
    attempts    smallint NOT NULL DEFAULT 0,    -- max 5 (FR-AUTH-02)
    verified_at timestamptz,
    pin_pending boolean NOT NULL DEFAULT false, -- OTP passed but 2FA PIN still required
    created_at  timestamptz NOT NULL DEFAULT now(),
    expires_at  timestamptz NOT NULL            -- created_at + 10 min
);
CREATE INDEX otp_challenges_expiry ON otp_challenges (expires_at);   -- 10-min sweeper

CREATE TABLE sessions (
    id           uuid PRIMARY KEY,
    device_id    uuid NOT NULL REFERENCES devices (id) ON DELETE CASCADE,
    refresh_hash bytea NOT NULL UNIQUE,          -- SHA-256 of the CURRENT refresh token
    rotated_from bytea,                          -- previous token's hash: a match here = reuse attack
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_used_at timestamptz,
    expires_at   timestamptz NOT NULL,           -- absolute session lifetime
    revoked_at   timestamptz
);
CREATE INDEX sessions_by_device ON sessions (device_id);
CREATE INDEX sessions_rotated_from ON sessions (rotated_from) WHERE rotated_from IS NOT NULL;
