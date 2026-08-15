// Package adapters implements polls.Store (polls + poll_votes) over PostgreSQL.
package adapters

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/whatsapp-v2/server/internal/polls"
)

// Store implements polls.Store over the polls + poll_votes tables (migration
// 000018).
type Store struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

func (s *Store) Create(ctx context.Context, p polls.Poll) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO polls (id, conversation_id, creator_id, option_count, multi, closed, closes_at, created_at)
		 VALUES ($1, $2, $3, $4, $5, false, $6, $7)`,
		p.ID, p.ConversationID, p.CreatorID, p.OptionCount, p.Multi, p.ClosesAt, p.CreatedAt)
	return err
}

func (s *Store) Get(ctx context.Context, id string) (polls.Poll, error) {
	var p polls.Poll
	err := s.pool.QueryRow(ctx,
		`SELECT id, conversation_id, creator_id, option_count, multi, closed, closes_at, created_at
		 FROM polls WHERE id = $1`, id).
		Scan(&p.ID, &p.ConversationID, &p.CreatorID, &p.OptionCount, &p.Multi, &p.Closed, &p.ClosesAt, &p.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return polls.Poll{}, polls.ErrNotFound
	}
	return p, err
}

func (s *Store) SetClosed(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `UPDATE polls SET closed = true WHERE id = $1`, id)
	return err
}

// ReplaceVotes swaps the voter's rows for the poll in one transaction, so a
// re-vote never leaves a partial selection.
func (s *Store) ReplaceVotes(ctx context.Context, pollID, voterID string, indices []int) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `DELETE FROM poll_votes WHERE poll_id = $1 AND voter_id = $2`, pollID, voterID); err != nil {
		return err
	}
	for _, i := range indices {
		if _, err := tx.Exec(ctx,
			`INSERT INTO poll_votes (poll_id, voter_id, option_index) VALUES ($1, $2, $3)`,
			pollID, voterID, i); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *Store) Tally(ctx context.Context, pollID string, optionCount int, voterID string) ([]int, int, []int, error) {
	counts := make([]int, optionCount)
	// (poll_id, voter_id, option_index) is unique, so count(*) per index is the
	// distinct-voter count for that option.
	rows, err := s.pool.Query(ctx,
		`SELECT option_index, count(*) FROM poll_votes WHERE poll_id = $1 GROUP BY option_index`, pollID)
	if err != nil {
		return nil, 0, nil, err
	}
	for rows.Next() {
		var idx, c int
		if err := rows.Scan(&idx, &c); err != nil {
			rows.Close()
			return nil, 0, nil, err
		}
		if idx >= 0 && idx < optionCount {
			counts[idx] = c
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, 0, nil, err
	}

	var total int
	if err := s.pool.QueryRow(ctx,
		`SELECT count(DISTINCT voter_id) FROM poll_votes WHERE poll_id = $1`, pollID).Scan(&total); err != nil {
		return nil, 0, nil, err
	}

	mrows, err := s.pool.Query(ctx,
		`SELECT option_index FROM poll_votes WHERE poll_id = $1 AND voter_id = $2 ORDER BY option_index`, pollID, voterID)
	if err != nil {
		return nil, 0, nil, err
	}
	defer mrows.Close()
	var mine []int
	for mrows.Next() {
		var idx int
		if err := mrows.Scan(&idx); err != nil {
			return nil, 0, nil, err
		}
		mine = append(mine, idx)
	}
	return counts, total, mine, mrows.Err()
}
