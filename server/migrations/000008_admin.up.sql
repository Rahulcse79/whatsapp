-- Trust & safety + operations: reports, immutable audit log, feature flags.
-- Contract: HLD §15.6; Docs/06-security/security-architecture.md §4.

CREATE TABLE reports (
    id                   uuid PRIMARY KEY,
    reporter_id          uuid REFERENCES users (id) ON DELETE SET NULL,
    target_user_id       uuid REFERENCES users (id) ON DELETE SET NULL,
    reason               smallint NOT NULL,
    note                 text,
    disclosed_ciphertext bytea,                    -- ONLY with the reporter's explicit consent (FR-ADMIN-05)
    state                smallint NOT NULL DEFAULT 0, -- 0 open | 1 actioned | 2 dismissed
    created_at           timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX reports_open ON reports (created_at) WHERE state = 0;

CREATE TABLE audit_log (
    id     bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    actor  uuid NOT NULL,
    action text NOT NULL,
    target uuid,
    reason text,
    at     timestamptz NOT NULL DEFAULT now()
);
-- Append-only: no role ever receives UPDATE/DELETE (grants bootstrap,
-- deploy/T0.22, enforces it; PUBLIC revoke here is defense in depth).
REVOKE UPDATE, DELETE, TRUNCATE ON audit_log FROM PUBLIC;

CREATE TABLE feature_flags (
    flag       text PRIMARY KEY,
    rules      jsonb NOT NULL,                     -- targeting rules + rollout %
    updated_by uuid,
    updated_at timestamptz NOT NULL DEFAULT now()
);
