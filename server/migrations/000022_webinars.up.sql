-- Webinars / live mode (T9.02): 1-to-many sessions with a waiting room,
-- roles (attendee/speaker/host), raise-hand, attendance (join/leave times), and
-- Q&A. Media rides LiveKit; role-scoped join tokens enforce the 1-to-many shape.

CREATE TABLE webinars (
    id         uuid PRIMARY KEY,
    title      text NOT NULL,
    host_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    room_id    text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    ended_at   timestamptz
);
CREATE INDEX webinars_by_host ON webinars (host_id);

CREATE TABLE webinar_participants (
    webinar_id  uuid NOT NULL REFERENCES webinars (id) ON DELETE CASCADE,
    user_id     uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    role        smallint NOT NULL DEFAULT 0,   -- 0 attendee | 1 speaker | 2 host
    status      smallint NOT NULL DEFAULT 0,   -- 0 waiting | 1 admitted | 2 left
    hand_raised boolean NOT NULL DEFAULT false,
    joined_at   timestamptz NOT NULL DEFAULT now(),
    left_at     timestamptz,
    PRIMARY KEY (webinar_id, user_id)
);

CREATE TABLE webinar_questions (
    id         uuid PRIMARY KEY,
    webinar_id uuid NOT NULL REFERENCES webinars (id) ON DELETE CASCADE,
    asker_id   uuid NOT NULL,
    body       text NOT NULL,
    answered   boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX webinar_questions_by_webinar ON webinar_questions (webinar_id);

-- Upvotes are distinct voters (count over this table); re-voting is idempotent.
CREATE TABLE webinar_question_votes (
    question_id uuid NOT NULL REFERENCES webinar_questions (id) ON DELETE CASCADE,
    voter_id    uuid NOT NULL,
    PRIMARY KEY (question_id, voter_id)
);
