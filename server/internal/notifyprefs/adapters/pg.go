// Package adapters implements notifyprefs.Store over PostgreSQL (notification_prefs,
// conversation_snooze, scheduled_notifications; migration 000030) plus the
// content-free email/SMS nudge senders.
package adapters

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/whatsapp-v2/server/internal/notifyprefs"
	"github.com/whatsapp-v2/server/internal/notifyprefs/domain"
)

type Store struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// nullMin maps a nullable minute-of-day column to the domain's -1 = off.
func nullMin(v *int16) int {
	if v == nil {
		return -1
	}
	return int(*v)
}

func minPtr(v int) *int16 {
	if v < 0 {
		return nil
	}
	m := int16(v)
	return &m
}

func (s *Store) GetPrefs(ctx context.Context, userID string) (domain.Prefs, error) {
	var (
		channels    int16
		qs, qe      *int16
		sound, vibr bool
	)
	err := s.pool.QueryRow(ctx,
		`SELECT channels, quiet_start, quiet_end, sound, vibrate
		 FROM notification_prefs WHERE user_id = $1`, userID).
		Scan(&channels, &qs, &qe, &sound, &vibr)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Prefs{}, notifyprefs.ErrNotFound
	}
	if err != nil {
		return domain.Prefs{}, err
	}
	return domain.Prefs{
		Channels:   domain.Channel(channels),
		QuietStart: nullMin(qs),
		QuietEnd:   nullMin(qe),
		Sound:      sound,
		Vibrate:    vibr,
	}, nil
}

func (s *Store) UpsertPrefs(ctx context.Context, userID string, p domain.Prefs) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO notification_prefs (user_id, channels, quiet_start, quiet_end, sound, vibrate, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, now())
		 ON CONFLICT (user_id) DO UPDATE SET
		   channels = EXCLUDED.channels, quiet_start = EXCLUDED.quiet_start,
		   quiet_end = EXCLUDED.quiet_end, sound = EXCLUDED.sound,
		   vibrate = EXCLUDED.vibrate, updated_at = now()`,
		userID, int16(p.Channels), minPtr(p.QuietStart), minPtr(p.QuietEnd), p.Sound, p.Vibrate)
	return err
}

func (s *Store) SetSnooze(ctx context.Context, userID, conversationID string, until time.Time) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO conversation_snooze (user_id, conversation_id, until)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (user_id, conversation_id) DO UPDATE SET until = EXCLUDED.until`,
		userID, conversationID, until)
	return err
}

func (s *Store) ClearSnooze(ctx context.Context, userID, conversationID string) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM conversation_snooze WHERE user_id = $1 AND conversation_id = $2`, userID, conversationID)
	return err
}

func (s *Store) GetSnooze(ctx context.Context, userID, conversationID string) (time.Time, error) {
	var until time.Time
	err := s.pool.QueryRow(ctx,
		`SELECT until FROM conversation_snooze WHERE user_id = $1 AND conversation_id = $2`,
		userID, conversationID).Scan(&until)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, nil // not snoozed
	}
	return until, err
}

func (s *Store) CreateScheduled(ctx context.Context, n notifyprefs.ScheduledNotification) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO scheduled_notifications (id, user_id, conversation_id, title, due_at, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		n.ID, n.UserID, nullStr(n.ConversationID), n.Title, n.DueAt, n.CreatedAt)
	return err
}

func (s *Store) ListScheduled(ctx context.Context, userID string) ([]notifyprefs.ScheduledNotification, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, user_id, conversation_id, title, due_at, fired_at, created_at
		 FROM scheduled_notifications WHERE user_id = $1 ORDER BY due_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanScheduled(rows)
}

func (s *Store) DeleteScheduled(ctx context.Context, userID, id string) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM scheduled_notifications WHERE id = $1 AND user_id = $2`, id, userID)
	return err
}

func (s *Store) DueBefore(ctx context.Context, cutoff time.Time, limit int) ([]notifyprefs.ScheduledNotification, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, user_id, conversation_id, title, due_at, fired_at, created_at
		 FROM scheduled_notifications
		 WHERE fired_at IS NULL AND due_at <= $1
		 ORDER BY due_at LIMIT $2`, cutoff, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanScheduled(rows)
}

func (s *Store) MarkFired(ctx context.Context, id string, at time.Time) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE scheduled_notifications SET fired_at = $2 WHERE id = $1`, id, at)
	return err
}

func scanScheduled(rows pgx.Rows) ([]notifyprefs.ScheduledNotification, error) {
	var out []notifyprefs.ScheduledNotification
	for rows.Next() {
		var (
			n       notifyprefs.ScheduledNotification
			conv    *string
			firedAt *time.Time
		)
		if err := rows.Scan(&n.ID, &n.UserID, &conv, &n.Title, &n.DueAt, &firedAt, &n.CreatedAt); err != nil {
			return nil, err
		}
		if conv != nil {
			n.ConversationID = *conv
		}
		n.FiredAt = firedAt
		out = append(out, n)
	}
	return out, rows.Err()
}

// nullStr maps "" to a SQL NULL for the optional conversation_id column.
func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}
