// Package adapters implements breakout.Store over PostgreSQL (live_sessions +
// breakout_rooms + breakout_assignments + recording_consents; 000024) and a
// no-op egress driver for dev (the real LiveKit egress adapter is the seam).
package adapters

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/whatsapp-v2/server/internal/breakout"
	"github.com/whatsapp-v2/server/internal/breakout/domain"
)

type Store struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

func (s *Store) CreateSession(ctx context.Context, ss breakout.Session) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO live_sessions (id, host_id, main_room, created_at) VALUES ($1, $2, $3, $4)`,
		ss.ID, ss.HostID, ss.MainRoom, ss.CreatedAt)
	return err
}

func (s *Store) GetSession(ctx context.Context, id string) (breakout.Session, error) {
	var ss breakout.Session
	var estate, ekind, rstate int16
	err := s.pool.QueryRow(ctx,
		`SELECT id, host_id, main_room, created_at, ended_at, egress_state, egress_kind, egress_url, egress_ref, recording_state
		 FROM live_sessions WHERE id = $1`, id).
		Scan(&ss.ID, &ss.HostID, &ss.MainRoom, &ss.CreatedAt, &ss.EndedAt, &estate, &ekind, &ss.EgressURL, &ss.EgressRef, &rstate)
	if errors.Is(err, pgx.ErrNoRows) {
		return breakout.Session{}, breakout.ErrNotFound
	}
	ss.EgressState, ss.EgressKind, ss.Recording = domain.EgressState(estate), domain.EgressKind(ekind), domain.RecordingState(rstate)
	return ss, err
}

func (s *Store) EndSession(ctx context.Context, id string, at time.Time) error {
	_, err := s.pool.Exec(ctx, `UPDATE live_sessions SET ended_at = $2 WHERE id = $1 AND ended_at IS NULL`, id, at)
	return err
}

func (s *Store) SetEgress(ctx context.Context, id string, state domain.EgressState, kind domain.EgressKind, url, ref string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE live_sessions SET egress_state = $2, egress_kind = $3, egress_url = $4, egress_ref = $5 WHERE id = $1`,
		id, int16(state), int16(kind), url, ref)
	return err
}

func (s *Store) SetRecording(ctx context.Context, id string, state domain.RecordingState) error {
	_, err := s.pool.Exec(ctx, `UPDATE live_sessions SET recording_state = $2 WHERE id = $1`, id, int16(state))
	return err
}

func (s *Store) CreateRoom(ctx context.Context, r breakout.Room) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO breakout_rooms (id, session_id, name, room, created_at) VALUES ($1, $2, $3, $4, $5)`,
		r.ID, r.SessionID, r.Name, r.Room, r.CreatedAt)
	return err
}

func (s *Store) ListRooms(ctx context.Context, sessionID string) ([]breakout.Room, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, session_id, name, room, created_at, closed_at
		 FROM breakout_rooms WHERE session_id = $1 AND closed_at IS NULL ORDER BY created_at`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []breakout.Room
	for rows.Next() {
		var r breakout.Room
		if err := rows.Scan(&r.ID, &r.SessionID, &r.Name, &r.Room, &r.CreatedAt, &r.ClosedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) CloseRooms(ctx context.Context, sessionID string, at time.Time) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE breakout_rooms SET closed_at = $2 WHERE session_id = $1 AND closed_at IS NULL`, sessionID, at)
	return err
}

func (s *Store) CountByRoom(ctx context.Context, sessionID string) (map[string]int, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT room_id, count(*) FROM breakout_assignments
		 WHERE session_id = $1 AND room_id IS NOT NULL GROUP BY room_id`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var roomID string
		var n int
		if err := rows.Scan(&roomID, &n); err != nil {
			return nil, err
		}
		out[roomID] = n
	}
	return out, rows.Err()
}

func (s *Store) SetAssignment(ctx context.Context, sessionID, userID string, roomID *string, at time.Time) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO breakout_assignments (session_id, user_id, room_id, assigned_at)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (session_id, user_id) DO UPDATE SET room_id = EXCLUDED.room_id, assigned_at = EXCLUDED.assigned_at`,
		sessionID, userID, roomID, at)
	return err
}

func (s *Store) GetAssignment(ctx context.Context, sessionID, userID string) (breakout.Assignment, error) {
	var a breakout.Assignment
	a.UserID = userID
	err := s.pool.QueryRow(ctx,
		`SELECT room_id, assigned_at FROM breakout_assignments WHERE session_id = $1 AND user_id = $2`, sessionID, userID).
		Scan(&a.RoomID, &a.AssignedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return breakout.Assignment{}, breakout.ErrNotFound
	}
	return a, err
}

func (s *Store) ClearAssignments(ctx context.Context, sessionID string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM breakout_assignments WHERE session_id = $1`, sessionID)
	return err
}

func (s *Store) ResetConsents(ctx context.Context, sessionID string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM recording_consents WHERE session_id = $1`, sessionID)
	return err
}

func (s *Store) SetConsent(ctx context.Context, sessionID, userID string, consented bool, at time.Time) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO recording_consents (session_id, user_id, consented, decided_at)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (session_id, user_id) DO UPDATE SET consented = EXCLUDED.consented, decided_at = EXCLUDED.decided_at`,
		sessionID, userID, consented, at)
	return err
}

func (s *Store) ListConsents(ctx context.Context, sessionID string) ([]breakout.Consent, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT user_id, consented, decided_at FROM recording_consents WHERE session_id = $1`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []breakout.Consent
	for rows.Next() {
		c := breakout.Consent{Decided: true}
		if err := rows.Scan(&c.UserID, &c.Consented, &c.DecidedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// NoopEgress is the dev egress driver: it records nothing and hands back a fake
// ref, so the control plane works end-to-end without a LiveKit egress service.
// The real adapter (LiveKit egress API) wires at the same media seam as calls.
type NoopEgress struct{}

func (NoopEgress) Start(_ context.Context, room string, _ domain.EgressKind, _ string) (string, error) {
	return "noop-egress:" + room, nil
}
func (NoopEgress) Stop(_ context.Context, _ string) error { return nil }
