// Package adapters implements webinar.Store over PostgreSQL (webinars +
// webinar_participants + webinar_questions + webinar_question_votes; 000022).
package adapters

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/whatsapp-v2/server/internal/webinar"
	"github.com/whatsapp-v2/server/internal/webinar/domain"
)

type Store struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

func (s *Store) Create(ctx context.Context, w webinar.Webinar) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO webinars (id, title, host_id, room_id, created_at) VALUES ($1, $2, $3, $4, $5)`,
		w.ID, w.Title, w.HostID, w.RoomID, w.CreatedAt)
	return err
}

func (s *Store) Get(ctx context.Context, id string) (webinar.Webinar, error) {
	var w webinar.Webinar
	err := s.pool.QueryRow(ctx,
		`SELECT id, title, host_id, room_id, created_at, ended_at FROM webinars WHERE id = $1`, id).
		Scan(&w.ID, &w.Title, &w.HostID, &w.RoomID, &w.CreatedAt, &w.EndedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return webinar.Webinar{}, webinar.ErrNotFound
	}
	return w, err
}

func (s *Store) End(ctx context.Context, id string, at time.Time) error {
	_, err := s.pool.Exec(ctx, `UPDATE webinars SET ended_at = $2 WHERE id = $1 AND ended_at IS NULL`, id, at)
	return err
}

func (s *Store) UpsertParticipant(ctx context.Context, webinarID string, p webinar.Participant) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO webinar_participants (webinar_id, user_id, role, status, hand_raised, joined_at)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT (webinar_id, user_id) DO NOTHING`,
		webinarID, p.UserID, int16(p.Role), int16(p.Status), p.HandRaised, p.JoinedAt)
	return err
}

func (s *Store) GetParticipant(ctx context.Context, webinarID, userID string) (webinar.Participant, error) {
	var p webinar.Participant
	var role, status int16
	err := s.pool.QueryRow(ctx,
		`SELECT user_id, role, status, hand_raised, joined_at, left_at
		 FROM webinar_participants WHERE webinar_id = $1 AND user_id = $2`, webinarID, userID).
		Scan(&p.UserID, &role, &status, &p.HandRaised, &p.JoinedAt, &p.LeftAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return webinar.Participant{}, webinar.ErrNotFound
	}
	p.Role, p.Status = domain.Role(role), domain.Status(status)
	return p, err
}

func (s *Store) SetStatus(ctx context.Context, webinarID, userID string, status domain.Status, leftAt *time.Time) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE webinar_participants SET status = $3, left_at = $4 WHERE webinar_id = $1 AND user_id = $2`,
		webinarID, userID, int16(status), leftAt)
	return err
}

func (s *Store) SetRole(ctx context.Context, webinarID, userID string, role domain.Role) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE webinar_participants SET role = $3 WHERE webinar_id = $1 AND user_id = $2`,
		webinarID, userID, int16(role))
	return err
}

func (s *Store) SetHand(ctx context.Context, webinarID, userID string, raised bool) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE webinar_participants SET hand_raised = $3 WHERE webinar_id = $1 AND user_id = $2`,
		webinarID, userID, raised)
	return err
}

func (s *Store) ListParticipants(ctx context.Context, webinarID string) ([]webinar.Participant, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT user_id, role, status, hand_raised, joined_at, left_at
		 FROM webinar_participants WHERE webinar_id = $1 ORDER BY role DESC, joined_at`, webinarID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []webinar.Participant
	for rows.Next() {
		var p webinar.Participant
		var role, status int16
		if err := rows.Scan(&p.UserID, &role, &status, &p.HandRaised, &p.JoinedAt, &p.LeftAt); err != nil {
			return nil, err
		}
		p.Role, p.Status = domain.Role(role), domain.Status(status)
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) CreateQuestion(ctx context.Context, webinarID string, q webinar.Question) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO webinar_questions (id, webinar_id, asker_id, body, created_at) VALUES ($1, $2, $3, $4, $5)`,
		q.ID, webinarID, q.AskerID, q.Body, q.CreatedAt)
	return err
}

func (s *Store) ListQuestions(ctx context.Context, webinarID string) ([]webinar.Question, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT q.id, q.asker_id, q.body, q.answered, q.created_at,
		        (SELECT count(*) FROM webinar_question_votes v WHERE v.question_id = q.id) AS upvotes
		 FROM webinar_questions q WHERE q.webinar_id = $1
		 ORDER BY upvotes DESC, q.created_at`, webinarID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []webinar.Question
	for rows.Next() {
		var q webinar.Question
		if err := rows.Scan(&q.ID, &q.AskerID, &q.Body, &q.Answered, &q.CreatedAt, &q.Upvotes); err != nil {
			return nil, err
		}
		out = append(out, q)
	}
	return out, rows.Err()
}

func (s *Store) UpvoteQuestion(ctx context.Context, webinarID, questionID, voterID string) error {
	// The webinar_id guard ensures the question belongs to this webinar.
	_, err := s.pool.Exec(ctx,
		`INSERT INTO webinar_question_votes (question_id, voter_id)
		 SELECT $1, $2 WHERE EXISTS (SELECT 1 FROM webinar_questions WHERE id = $1 AND webinar_id = $3)
		 ON CONFLICT (question_id, voter_id) DO NOTHING`,
		questionID, voterID, webinarID)
	return err
}

func (s *Store) AnswerQuestion(ctx context.Context, webinarID, questionID string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE webinar_questions SET answered = true WHERE id = $1 AND webinar_id = $2`, questionID, webinarID)
	return err
}
