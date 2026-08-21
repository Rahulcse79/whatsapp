-- Bot framework (T13.02): a webhook API. A user registers a bot (public @handle
-- + an https webhook) and gets a shared HMAC secret; the server delivers signed
-- events to the webhook when users interact with the bot. Bot integrations are
-- server-visible by design (talking to a bot opts that thread out of E2EE);
-- user↔user interactive messages stay E2EE on the client.

CREATE TABLE bots (
    id          uuid PRIMARY KEY,
    owner_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    handle      text NOT NULL UNIQUE,    -- public @handle, lowercase [a-z0-9_]{3,32}
    name        text NOT NULL,
    webhook_url text NOT NULL,           -- https endpoint we POST signed events to
    secret      text NOT NULL,           -- shared HMAC-SHA256 secret
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX bots_by_owner ON bots (owner_id);
