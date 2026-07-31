package notify

import (
	"sync"
	"time"
)

// Breaker defaults (notification-svc-lld §3): 5 failures within 30 s opens the
// breaker for 60 s. While open the pipeline nacks — JetStream holds the work
// (24 h retention) and drains it when the provider recovers.
const (
	DefaultThreshold = 5
	DefaultWindow    = 30 * time.Second
	DefaultCooldown  = 60 * time.Second
)

type breakerState int

const (
	stateClosed breakerState = iota
	stateOpen
	stateHalfOpen
)

// Breaker is a per-provider circuit breaker. Safe for concurrent use; the clock
// is injectable for tests.
type Breaker struct {
	threshold int
	window    time.Duration
	cooldown  time.Duration

	mu       sync.Mutex
	state    breakerState
	failures []time.Time // failure times within the window
	openedAt time.Time
	now      func() time.Time
}

func NewBreaker(threshold int, window, cooldown time.Duration) *Breaker {
	if threshold <= 0 {
		threshold = DefaultThreshold
	}
	if window <= 0 {
		window = DefaultWindow
	}
	if cooldown <= 0 {
		cooldown = DefaultCooldown
	}
	return &Breaker{threshold: threshold, window: window, cooldown: cooldown, now: time.Now}
}

// Allow reports whether a request may proceed. An open breaker admits exactly
// one probe once the cooldown elapses (half-open); further requests are blocked
// until that probe's outcome is recorded.
func (b *Breaker) Allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch b.state {
	case stateOpen:
		if b.now().Sub(b.openedAt) >= b.cooldown {
			b.state = stateHalfOpen
			return true // the single probe
		}
		return false
	case stateHalfOpen:
		return false
	default:
		return true
	}
}

// Record feeds back the outcome of an allowed request. Success closes the
// breaker; a half-open failure re-opens it; enough failures in the window open
// it.
func (b *Breaker) Record(success bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.now()
	if success {
		b.state = stateClosed
		b.failures = nil
		return
	}
	if b.state == stateHalfOpen {
		b.state = stateOpen
		b.openedAt = now
		return
	}
	cutoff := now.Add(-b.window)
	kept := b.failures[:0]
	for _, t := range b.failures {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	b.failures = append(kept, now)
	if len(b.failures) >= b.threshold {
		b.state = stateOpen
		b.openedAt = now
	}
}

// Open reports whether the breaker is currently blocking (observability/tests).
func (b *Breaker) Open() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.state != stateOpen {
		return false
	}
	return b.now().Sub(b.openedAt) < b.cooldown
}
