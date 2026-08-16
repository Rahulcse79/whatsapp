-- Advanced live sessions (T9.03): breakout rooms, streaming egress (RTMP/HLS),
-- and recording consent. Sits over the LiveKit media plane like calls/webinar —
-- breakout participants get a fresh per-room join token; egress + recording are
-- host-driven and never touch E2EE payloads (recording is on-device, gated by
-- consent). Multi-camera + 4K profiles are purely client-side.

CREATE TABLE live_sessions (
    id              uuid PRIMARY KEY,
    host_id         uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    main_room       text NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    ended_at        timestamptz,
    egress_state    smallint NOT NULL DEFAULT 0, -- 0 off | 1 live
    egress_kind     smallint NOT NULL DEFAULT 0, -- 0 rtmp | 1 hls
    egress_url      text NOT NULL DEFAULT '',
    egress_ref      text NOT NULL DEFAULT '',     -- opaque LiveKit egress id
    recording_state smallint NOT NULL DEFAULT 0  -- 0 off | 1 requested | 2 active
);
CREATE INDEX live_sessions_by_host ON live_sessions (host_id);

CREATE TABLE breakout_rooms (
    id         uuid PRIMARY KEY,
    session_id uuid NOT NULL REFERENCES live_sessions (id) ON DELETE CASCADE,
    name       text NOT NULL,
    room       text NOT NULL,   -- LiveKit room name (bo-…)
    created_at timestamptz NOT NULL DEFAULT now(),
    closed_at  timestamptz
);
CREATE INDEX breakout_rooms_open ON breakout_rooms (session_id) WHERE closed_at IS NULL;

-- One current assignment per participant; room_id NULL = the main room.
CREATE TABLE breakout_assignments (
    session_id  uuid NOT NULL REFERENCES live_sessions (id) ON DELETE CASCADE,
    user_id     uuid NOT NULL,
    room_id     uuid REFERENCES breakout_rooms (id) ON DELETE SET NULL,
    assigned_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (session_id, user_id)
);

-- Recording consent: one decision per participant (present = has a row).
CREATE TABLE recording_consents (
    session_id uuid NOT NULL REFERENCES live_sessions (id) ON DELETE CASCADE,
    user_id    uuid NOT NULL,
    consented  boolean NOT NULL,
    decided_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (session_id, user_id)
);
