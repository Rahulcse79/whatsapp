// Package ratelimit implements GCRA (generic cell rate algorithm) rate
// limiting — the single limiter used at every tier: per-IP edge, per-device
// sends, OTP, group fan-out, media quota (DS&A doc §7).
//
// GCRA stores one value per key (the theoretical arrival time, TAT), admits
// smooth rates without fixed-window boundary bursts, and decides in O(1).
// Two backends share the same math: MemoryLimiter (tests, single-pod tools)
// and ValkeyLimiter (production, atomic Lua — see valkey.go).
package ratelimit

import (
	"fmt"
	"sync"
	"time"
)

// Params defines a limit.
//
// Rate is the sustained events/second; Burst is the maximum number of events
// admitted instantaneously from idle (Burst 1 = strict pacing). Example: the
// per-device send limit "20 msg/s burst 40" is Params{Rate: 20, Burst: 40}.
type Params struct {
	Rate  float64
	Burst int
}

// Validate rejects unusable parameter combinations.
func (p Params) Validate() error {
	if p.Rate <= 0 {
		return fmt.Errorf("ratelimit: rate must be > 0, got %v", p.Rate)
	}
	if p.Burst < 1 {
		return fmt.Errorf("ratelimit: burst must be >= 1, got %d", p.Burst)
	}
	return nil
}

// emission is the interval between admitted events at the sustained rate
// (GCRA's "T").
func (p Params) emission() time.Duration {
	return time.Duration(float64(time.Second) / p.Rate)
}

// tolerance is the burst allowance (GCRA's "τ"): (Burst-1)·T admits exactly
// Burst events from idle.
func (p Params) tolerance() time.Duration {
	return time.Duration(p.Burst-1) * p.emission()
}

// Result of a limiter decision.
type Result struct {
	Allowed bool
	// RetryAfter is how long the caller must wait before the next attempt
	// can succeed; zero when Allowed. Surfaces to clients as retry_after_ms
	// (api-standards.md §2).
	RetryAfter time.Duration
}

// decide is the pure GCRA core shared by both backends.
// A denied request does not consume capacity (TAT is unchanged).
func decide(now, tat time.Time, p Params) (Result, time.Time) {
	if tat.Before(now) {
		tat = now
	}
	if ahead := tat.Sub(now); ahead > p.tolerance() {
		return Result{Allowed: false, RetryAfter: ahead - p.tolerance()}, tat
	}
	return Result{Allowed: true}, tat.Add(p.emission())
}

// MemoryLimiter is the in-process backend: correct for tests and single-pod
// tooling only. Production multi-pod limiting must use ValkeyLimiter —
// per-pod memory would multiply every limit by the replica count.
type MemoryLimiter struct {
	mu   sync.Mutex
	tats map[string]time.Time
	now  func() time.Time
}

// NewMemoryLimiter returns a MemoryLimiter using the real clock.
func NewMemoryLimiter() *MemoryLimiter {
	return &MemoryLimiter{tats: make(map[string]time.Time), now: time.Now}
}

// Allow records an attempt against key and reports the decision.
func (m *MemoryLimiter) Allow(key string, p Params) (Result, error) {
	if err := p.Validate(); err != nil {
		return Result{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	res, newTAT := decide(m.now(), m.tats[key], p)
	if res.Allowed {
		m.tats[key] = newTAT
	}
	return res, nil
}
