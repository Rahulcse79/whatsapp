-- Monetization backbone (T15.05): premium subscriptions, paid channels, and the
-- person-to-person transfer seam.
--
-- WHAT IS DELIBERATELY ABSENT FROM THESE TABLES: any card data. No PAN, no
-- expiry, no CVV, no cardholder name, not even a last-4. The payer enters their
-- details on the payment provider's own surface and we keep only opaque
-- processor references — that is what keeps this deployment in PCI-DSS SAQ-A,
-- and it means a dump of these tables exposes no payment instrument.

CREATE TABLE payments (
    id          uuid        PRIMARY KEY,
    user_id     uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,   -- the payer
    purpose     text        NOT NULL,          -- premium | channel_sub | p2p_transfer
    amount_cents bigint     NOT NULL CHECK (amount_cents > 0),
    currency    text        NOT NULL CHECK (char_length(currency) = 3),
    status      text        NOT NULL,          -- pending | succeeded | failed | canceled | refunded
    psp_ref     text,                          -- the processor's opaque id; NULL until checkout is created
    subject     text,                          -- channel id, payee user id, or NULL
    memo        text,                          -- user note; card-data guarded at the API boundary
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT payments_purpose_known CHECK (purpose IN ('premium', 'channel_sub', 'p2p_transfer')),
    CONSTRAINT payments_status_known  CHECK (status IN ('pending', 'succeeded', 'failed', 'canceled', 'refunded'))
);
CREATE INDEX payments_by_user ON payments (user_id, created_at DESC);
-- Webhook lookup is by processor reference, and a reference identifies exactly
-- one payment.
CREATE UNIQUE INDEX payments_by_psp_ref ON payments (psp_ref) WHERE psp_ref IS NOT NULL;
-- Admin console: filter by status, newest first.
CREATE INDEX payments_by_status ON payments (status, created_at DESC);

CREATE TABLE subscriptions (
    id           uuid        PRIMARY KEY,
    user_id      uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    purpose      text        NOT NULL,         -- premium | channel_sub
    subject      text        NOT NULL DEFAULT '',  -- channel id for channel_sub, '' for premium
    psp_ref      text,
    active       boolean     NOT NULL DEFAULT true,
    started_at   timestamptz NOT NULL DEFAULT now(),
    expires_at   timestamptz NOT NULL,
    canceled_at  timestamptz,
    CONSTRAINT subscriptions_purpose_known CHECK (purpose IN ('premium', 'channel_sub'))
);
-- The entitlement check is "does this user hold this thing right now", so it is
-- the lookup that must be fast.
CREATE INDEX subscriptions_entitlement ON subscriptions (user_id, purpose, subject, expires_at DESC)
    WHERE active;
-- One live subscription per user per thing: a double-charge must not create a
-- second overlapping entitlement.
CREATE UNIQUE INDEX subscriptions_one_active ON subscriptions (user_id, purpose, subject)
    WHERE active AND canceled_at IS NULL;

-- Webhook idempotency. A processor may deliver the same event many times; the
-- primary key is what makes replay a no-op instead of a double entitlement.
CREATE TABLE payment_events (
    event_id     text        PRIMARY KEY,      -- the PSP's own event id
    psp_ref      text,
    raw_kind     text        NOT NULL,         -- provider's event name, for the audit trail
    processed_at timestamptz NOT NULL DEFAULT now()
);
-- Retention: these exist only to deduplicate redeliveries, which arrive within
-- hours, so the table is swept rather than kept forever.
CREATE INDEX payment_events_processed_at ON payment_events (processed_at);
