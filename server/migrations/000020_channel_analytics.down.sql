DROP TABLE IF EXISTS channel_subscriptions;
ALTER TABLE channels DROP COLUMN IF EXISTS price_cents;
ALTER TABLE channels DROP COLUMN IF EXISTS premium;
ALTER TABLE channel_posts DROP COLUMN IF EXISTS views;
