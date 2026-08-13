package adapters

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/whatsapp-v2/server/internal/analytics"
	"github.com/whatsapp-v2/server/internal/analytics/domain"
)

// Distinct counts distinct users with Valkey HyperLogLog — a fixed-size sketch
// (~12 KB, ~0.8 % error) that answers "how many distinct" without storing WHICH
// users. That is the privacy guarantee: DAU/MAU exist, per-user activity does
// not (HLD §18.1). Buckets expire after the MAU window + slack, since once a day
// is rolled up into analytics_daily the sketch is no longer needed.
type Distinct struct {
	client *redis.Client
	ttl    time.Duration
}

func NewDistinct(client *redis.Client) *Distinct {
	return &Distinct{client: client, ttl: (domain.MAUWindow + 7) * 24 * time.Hour}
}

var _ analytics.Distinct = (*Distinct)(nil)

func hllKey(bucket string) string { return "analytics:hll:" + bucket }

// Add records a user in a bucket (PFADD is idempotent per user) and refreshes
// the bucket's TTL.
func (d *Distinct) Add(ctx context.Context, bucket, userID string) error {
	key := hllKey(bucket)
	pipe := d.client.TxPipeline()
	pipe.PFAdd(ctx, key, userID)
	pipe.Expire(ctx, key, d.ttl)
	_, err := pipe.Exec(ctx)
	return err
}

// Count is the cardinality of the union of the given buckets — PFCOUNT over
// several keys merges them without a persistent temp key, so MAU is just the
// count over 30 daily buckets.
func (d *Distinct) Count(ctx context.Context, buckets ...string) (int64, error) {
	if len(buckets) == 0 {
		return 0, nil
	}
	keys := make([]string, len(buckets))
	for i, b := range buckets {
		keys[i] = hllKey(b)
	}
	return d.client.PFCount(ctx, keys...).Result()
}
