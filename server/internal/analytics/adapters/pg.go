// Package adapters implements analytics' durable rollup store over the
// analytics_daily table (migration 000017) and the distinct-user sketch over
// Valkey HyperLogLog. Neither stores a user id — metadata only (HLD §18.1).
package adapters

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/whatsapp-v2/server/internal/analytics"
)

// Rollups is the analytics_daily store: (day, metric) → value.
type Rollups struct{ pool *pgxpool.Pool }

func NewRollups(pool *pgxpool.Pool) *Rollups { return &Rollups{pool: pool} }

var _ analytics.Rollups = (*Rollups)(nil)

// IncrDaily adds delta to a counter cell, creating it at delta if absent.
func (r *Rollups) IncrDaily(ctx context.Context, day time.Time, metric string, delta int64) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO analytics_daily (day, metric, value) VALUES ($1, $2, $3)
		 ON CONFLICT (day, metric) DO UPDATE SET value = analytics_daily.value + EXCLUDED.value`,
		day, metric, delta)
	return err
}

// SetDaily overwrites a computed cell (DAU/MAU) — idempotent by construction.
func (r *Rollups) SetDaily(ctx context.Context, day time.Time, metric string, value int64) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO analytics_daily (day, metric, value) VALUES ($1, $2, $3)
		 ON CONFLICT (day, metric) DO UPDATE SET value = EXCLUDED.value`,
		day, metric, value)
	return err
}

func (r *Rollups) Query(ctx context.Context, from, to time.Time) ([]analytics.DailyValue, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT day, metric, value FROM analytics_daily WHERE day >= $1 AND day <= $2 ORDER BY day, metric`,
		from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []analytics.DailyValue
	for rows.Next() {
		var v analytics.DailyValue
		if err := rows.Scan(&v.Day, &v.Metric, &v.Value); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (r *Rollups) PurgeOlderThan(ctx context.Context, cutoff time.Time) (int64, error) {
	tag, err := r.pool.Exec(ctx, `DELETE FROM analytics_daily WHERE day < $1`, cutoff)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
