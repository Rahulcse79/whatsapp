package adapters

import (
	"context"

	"github.com/whatsapp-v2/server/internal/contacts"
	"github.com/whatsapp-v2/server/internal/platform/ratelimit"
)

// SearchRate gates username search per user (threat-model T11). Interactive pace
// for type-ahead while capping directory scraping: ~1/s sustained, burst 10.
type SearchRate struct {
	limiter *ratelimit.ValkeyLimiter
	params  ratelimit.Params
}

func NewSearchRate(limiter *ratelimit.ValkeyLimiter) *SearchRate {
	return &SearchRate{limiter: limiter, params: ratelimit.Params{Rate: 1.0, Burst: 10}}
}

func (r *SearchRate) Allow(ctx context.Context, key string) (bool, error) {
	res, err := r.limiter.Allow(ctx, key, r.params)
	if err != nil {
		return false, err
	}
	return res.Allowed, nil
}

var _ contacts.Rate = (*SearchRate)(nil)

// SyncDailyLimiter enforces a per-key daily cap via GCRA — the same technique
// auth uses for the OTP "N/day" limits (service.go): Rate = limit/86400 events
// per second with Burst = limit admits `limit` calls from idle, then refills one
// slot roughly every 86400/limit seconds.
type SyncDailyLimiter struct{ limiter *ratelimit.ValkeyLimiter }

func NewSyncDailyLimiter(limiter *ratelimit.ValkeyLimiter) *SyncDailyLimiter {
	return &SyncDailyLimiter{limiter: limiter}
}

func (l *SyncDailyLimiter) AllowDaily(ctx context.Context, key string, limit int) (bool, error) {
	res, err := l.limiter.Allow(ctx, key, ratelimit.Params{Rate: float64(limit) / 86400.0, Burst: limit})
	if err != nil {
		return false, err
	}
	return res.Allowed, nil
}

var _ contacts.DailyLimiter = (*SyncDailyLimiter)(nil)
