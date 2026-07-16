-- QR device-linking handoff (e2ee-design §5, sequence-diagrams §8).
-- A new device creates a pending link and shows its token in a QR; the
-- primary device scans it and approves with a signature over the new
-- device's identity key; the new device then completes to obtain tokens.
CREATE TABLE device_links (
    link_token   text PRIMARY KEY,             -- high-entropy secret shown in the QR
    platform     smallint NOT NULL,
    device_name  text,
    identity_key bytea NOT NULL,               -- the NEW device's public identity key
    state        smallint NOT NULL DEFAULT 0,  -- 0 pending | 1 approved | 2 consumed
    approved_by  uuid,                          -- primary device that approved
    user_id      uuid,                          -- set at approval
    device_id    uuid,                          -- the new device's id, set at approval
    cert         bytea,                         -- primary's signature over identity_key
    created_at   timestamptz NOT NULL DEFAULT now(),
    expires_at   timestamptz NOT NULL           -- created_at + a few minutes
);
CREATE INDEX device_links_expiry ON device_links (expires_at);
