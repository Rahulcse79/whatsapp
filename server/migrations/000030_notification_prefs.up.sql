-- Multi-channel notification preferences (T14.01): server-authoritative so a
-- user's channels, quiet hours, snoozes, and scheduled reminders apply across
-- all their devices (the old client-only mute was device-local). Preferences
-- carry NO message content — they only decide WHETHER and by WHICH channel a
-- content-free wake/nudge is delivered (the E2EE no-plaintext invariant holds).

-- One row per user: which channels may fire, an optional quiet-hours window
-- (minutes since local midnight; wrap-around allowed), and sound/vibrate.
CREATE TABLE notification_prefs (
    user_id      uuid PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    channels     smallint    NOT NULL DEFAULT 9,   -- bitmask: 1 push | 2 email | 4 sms | 8 desktop (default push+desktop)
    quiet_start  smallint,                          -- minute-of-day [0,1439]; NULL = quiet hours off
    quiet_end    smallint,                          -- minute-of-day [0,1439]; NULL = quiet hours off
    sound        boolean     NOT NULL DEFAULT true,
    vibrate      boolean     NOT NULL DEFAULT true,
    updated_at   timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT quiet_both_or_neither CHECK ((quiet_start IS NULL) = (quiet_end IS NULL)),
    CONSTRAINT quiet_start_range CHECK (quiet_start IS NULL OR (quiet_start BETWEEN 0 AND 1439)),
    CONSTRAINT quiet_end_range   CHECK (quiet_end   IS NULL OR (quiet_end   BETWEEN 0 AND 1439))
);

-- Per-conversation snooze (server-authoritative mute-until). A row exists only
-- while a conversation is snoozed; clearing deletes it.
CREATE TABLE conversation_snooze (
    user_id         uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    conversation_id uuid        NOT NULL,
    until           timestamptz NOT NULL,   -- snoozed until this instant (UTC)
    PRIMARY KEY (user_id, conversation_id)
);

-- Scheduled reminder notifications: a user schedules a content-free nudge to
-- themselves at a future time (e.g. "remind me about this chat at 5pm"). A
-- due-scan surfaces them; fired rows are marked, cancelled rows are deleted.
CREATE TABLE scheduled_notifications (
    id              uuid        PRIMARY KEY,
    user_id         uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    conversation_id uuid,                   -- optional deep-link target
    title           text        NOT NULL,
    due_at          timestamptz NOT NULL,
    fired_at        timestamptz,            -- NULL until the due-scan fires it
    created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX scheduled_notifications_by_user ON scheduled_notifications (user_id, due_at);
-- Due-scan lane: pending (not yet fired) reminders ordered by due time.
CREATE INDEX scheduled_notifications_due ON scheduled_notifications (due_at) WHERE fired_at IS NULL;
