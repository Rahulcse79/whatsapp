-- Channel analytics + monetization (T7.03).
--
-- Analytics are privacy-preserving AGGREGATES: a post carries a plain view
-- counter (no per-viewer log), and insights are sums over the channel's own
-- posts/reactions/comments/followers. No behavioural per-user rows exist — the
-- only per-user channel record is a subscription, which is a billing fact.
ALTER TABLE channel_posts ADD COLUMN views bigint NOT NULL DEFAULT 0;

-- Premium: a channel owner may gate the channel behind a monthly subscription.
-- price_cents is the monthly price (0 = free even when premium is toggled on).
ALTER TABLE channels ADD COLUMN premium boolean NOT NULL DEFAULT false;
ALTER TABLE channels ADD COLUMN price_cents integer NOT NULL DEFAULT 0;

-- Premium subscriptions. The CHARGE itself is an external-processor seam:
-- payment_ref holds the processor's reference (dev records a placeholder). A
-- subscription is time-boxed (expires_at); re-subscribing extends it.
CREATE TABLE channel_subscriptions (
    channel_id  uuid NOT NULL REFERENCES channels (id) ON DELETE CASCADE,
    user_id     uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    payment_ref text NOT NULL DEFAULT '',
    started_at  timestamptz NOT NULL DEFAULT now(),
    expires_at  timestamptz NOT NULL,
    PRIMARY KEY (channel_id, user_id)
);
CREATE INDEX channel_subscriptions_by_user ON channel_subscriptions (user_id);
