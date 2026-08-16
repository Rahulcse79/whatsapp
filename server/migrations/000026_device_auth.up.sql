-- Device-auth hardening (T10.02): WebAuthn passkeys (2FA / step-up), single-use
-- challenges, and a login-event audit that flags sign-ins from a new IP. Passkey
-- verification is standard public-key crypto (Go stdlib); no E2EE key material
-- is stored here.

CREATE TABLE passkey_credentials (
    id           text PRIMARY KEY,             -- credential id (base64url)
    user_id      uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    alg          integer NOT NULL,             -- COSE alg (-7 ES256 | -8 EdDSA)
    public_key   bytea NOT NULL,               -- ES256 x||y (64B) or EdDSA (32B)
    sign_count   bigint NOT NULL DEFAULT 0,
    name         text NOT NULL DEFAULT 'Passkey',
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_used_at timestamptz
);
CREATE INDEX passkey_credentials_by_user ON passkey_credentials (user_id);

CREATE TABLE webauthn_challenges (
    value      text PRIMARY KEY,               -- base64url challenge
    user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    purpose    text NOT NULL,                  -- register | login
    expires_at timestamptz NOT NULL
);

CREATE TABLE login_events (
    id         uuid PRIMARY KEY,
    user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    device_id  uuid,
    ip         text NOT NULL DEFAULT '',
    user_agent text NOT NULL DEFAULT '',
    at         timestamptz NOT NULL DEFAULT now(),
    suspicious boolean NOT NULL DEFAULT false
);
CREATE INDEX login_events_by_user ON login_events (user_id, at DESC);
