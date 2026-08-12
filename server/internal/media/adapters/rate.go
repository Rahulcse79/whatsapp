package adapters

import (
	"context"

	"github.com/whatsapp-v2/server/internal/media"
	"github.com/whatsapp-v2/server/internal/platform/ratelimit"
)

// Rate gates create-upload at 30/hr/user (media-svc-lld §3) via the GCRA limiter.
type Rate struct {
	limiter *ratelimit.ValkeyLimiter
	params  ratelimit.Params
}

func NewRate(limiter *ratelimit.ValkeyLimiter) *Rate {
	// 30 uploads/hour → 30/3600 events/second, burst 30.
	return &Rate{limiter: limiter, params: ratelimit.Params{Rate: 30.0 / 3600.0, Burst: 30}}
}

func (r *Rate) Allow(ctx context.Context, key string) (bool, error) {
	res, err := r.limiter.Allow(ctx, key, r.params)
	if err != nil {
		return false, err
	}
	return res.Allowed, nil
}

var _ media.Rate = (*Rate)(nil)

// SearchRate gates GIF search (FR-MED-05) at a more interactive rate than
// uploads — ~1/sec sustained with a burst of 20, ample for a typing-ahead picker
// while still capping automated scraping through the proxy.
type SearchRate struct {
	limiter *ratelimit.ValkeyLimiter
	params  ratelimit.Params
}

func NewSearchRate(limiter *ratelimit.ValkeyLimiter) *SearchRate {
	return &SearchRate{limiter: limiter, params: ratelimit.Params{Rate: 1.0, Burst: 20}}
}

func (r *SearchRate) Allow(ctx context.Context, key string) (bool, error) {
	res, err := r.limiter.Allow(ctx, key, r.params)
	if err != nil {
		return false, err
	}
	return res.Allowed, nil
}

var _ media.Rate = (*SearchRate)(nil)
