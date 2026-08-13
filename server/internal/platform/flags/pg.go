package flags

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PGStore reads rules from feature_flags (migration 000008). It is the read
// side only — mutations run in the admin plane, co-transactional with audit.
type PGStore struct{ pool *pgxpool.Pool }

func NewPGStore(pool *pgxpool.Pool) *PGStore { return &PGStore{pool: pool} }

var _ Store = (*PGStore)(nil)

func (s *PGStore) Get(ctx context.Context, flag string) (Rule, bool, error) {
	var raw []byte
	err := s.pool.QueryRow(ctx, `SELECT rules FROM feature_flags WHERE flag = $1`, flag).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return Rule{}, false, nil
	}
	if err != nil {
		return Rule{}, false, err
	}
	var rule Rule
	if err := json.Unmarshal(raw, &rule); err != nil {
		return Rule{}, false, err
	}
	return rule, true, nil
}

func (s *PGStore) List(ctx context.Context) ([]Named, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT flag, rules, COALESCE(updated_by, ''), updated_at FROM feature_flags ORDER BY flag`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Named
	for rows.Next() {
		var n Named
		var raw []byte
		if err := rows.Scan(&n.Flag, &raw, &n.UpdatedBy, &n.UpdatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &n.Rule); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}
