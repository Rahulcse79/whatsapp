-- Call history (metadata only, 90-day retention) + push token registry.

CREATE TABLE call_records (
    id           uuid PRIMARY KEY,
    room_id      text NOT NULL,
    kind         smallint NOT NULL,                -- 0 voice | 1 video | 2 ptt
    initiator    uuid NOT NULL,                    -- no FK: records outlive deleted accounts as tombstoned ids
    participants uuid[] NOT NULL,
    started_at   timestamptz,
    ended_at     timestamptz,
    outcome      smallint NOT NULL                 -- 0 completed | 1 missed | 2 declined | 3 failed
);

-- Call-history queries ("my calls"); 90-day purge job scans started_at.
CREATE INDEX calls_by_user ON call_records USING gin (participants);
CREATE INDEX calls_by_time ON call_records (started_at);

CREATE TABLE push_tokens (
    device_id     uuid PRIMARY KEY REFERENCES devices (id) ON DELETE CASCADE,
    provider      smallint NOT NULL,               -- 0 fcm | 1 apns | 2 apns_voip | 3 ntfy | 4 webpush
    token         text NOT NULL,
    updated_at    timestamptz NOT NULL DEFAULT now(),
    failing_since timestamptz                      -- provider feedback; purge after 30 d failing
);
