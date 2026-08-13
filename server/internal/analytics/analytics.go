// Package analytics is the metadata-only product-analytics plane (HLD §18.1). A
// small NATS consumer aggregates privacy-preserving events into daily
// PostgreSQL rollup tables and exports the headline numbers as Prometheus
// metrics for Grafana. It never sees message content, builds no per-user
// profile, and counts distinct users through a sketch (no per-user rows).
package analytics

import (
	"context"
	"time"

	"github.com/whatsapp-v2/server/internal/analytics/domain"
)

// Event is one metadata-only product signal. UserID is used ONLY to feed the
// distinct-user sketch (DAU/MAU) and is never persisted per user. There is no
// content field — metadata-only is enforced by the type's shape.
type Event struct {
	Kind   domain.EventKind `json:"kind"`
	UserID string           `json:"user_id,omitempty"` // distinct sketch only; never stored per-user
	Label  string           `json:"label,omitempty"`   // bounded dimension (e.g. flag name)
	Delta  int64            `json:"delta,omitempty"`   // counter increment (defaults to 1)
	At     time.Time        `json:"at"`
}

// DailyValue is one rollup cell: a metric's value on a day.
type DailyValue struct {
	Day    time.Time `json:"day"`
	Metric string    `json:"metric"`
	Value  int64     `json:"value"`
}

// Rollups is the durable daily aggregate store (analytics_daily). IncrDaily adds
// to a counter cell; SetDaily overwrites a computed cell (DAU/MAU). Neither ever
// stores a user id.
type Rollups interface {
	IncrDaily(ctx context.Context, day time.Time, metric string, delta int64) error
	SetDaily(ctx context.Context, day time.Time, metric string, value int64) error
	Query(ctx context.Context, from, to time.Time) ([]DailyValue, error)
	PurgeOlderThan(ctx context.Context, cutoff time.Time) (int64, error)
}

// Distinct is the probabilistic distinct-user counter (Valkey HyperLogLog):
// Add records that a user was seen in a bucket; Count is the cardinality of the
// union of buckets. The sketch stores no user ids — it cannot answer "was user
// X active", only "how many distinct users", which is the whole privacy point.
type Distinct interface {
	Add(ctx context.Context, bucket, userID string) error
	Count(ctx context.Context, buckets ...string) (int64, error)
}

// Emitter publishes events onto the analytics subject (fire-and-forget from the
// producer's side). The consumer aggregates them.
type Emitter interface {
	Emit(ctx context.Context, e Event) error
}

// hllBucket is the sketch key for a distinct kind on a day, e.g.
// "active_user:2026-08-13".
func hllBucket(kind domain.EventKind, day time.Time) string {
	return string(kind) + ":" + domain.DayKey(day)
}
