package ratelimit

import (
	"testing"
	"time"
)

// fakeClock lets tests move time deterministically — no sleeps.
type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time            { return c.t }
func (c *fakeClock) advance(d time.Duration)   { c.t = c.t.Add(d) }
func newTestLimiter() (*MemoryLimiter, *fakeClock) {
	c := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	return &MemoryLimiter{tats: make(map[string]time.Time), now: c.now}, c
}

func TestBurst_AdmitsExactlyBurstFromIdle(t *testing.T) {
	l, _ := newTestLimiter()
	p := Params{Rate: 10, Burst: 3}

	for i := 0; i < 3; i++ {
		r, err := l.Allow("k", p)
		if err != nil || !r.Allowed {
			t.Fatalf("attempt %d: allowed=%v err=%v, want allowed", i+1, r.Allowed, err)
		}
	}
	r, _ := l.Allow("k", p)
	if r.Allowed {
		t.Fatal("attempt 4 allowed instantly; burst=3 must deny it")
	}
	// Next slot frees after one emission interval (100 ms at 10/s).
	if want := 100 * time.Millisecond; r.RetryAfter != want {
		t.Fatalf("RetryAfter = %v, want %v", r.RetryAfter, want)
	}
}

func TestDeny_DoesNotConsumeCapacity(t *testing.T) {
	l, c := newTestLimiter()
	p := Params{Rate: 10, Burst: 1}

	if r, _ := l.Allow("k", p); !r.Allowed {
		t.Fatal("first attempt must pass")
	}
	// Hammering while denied must not push the retry horizon further out.
	first, _ := l.Allow("k", p)
	for i := 0; i < 50; i++ {
		if r, _ := l.Allow("k", p); r.RetryAfter > first.RetryAfter {
			t.Fatalf("denied attempts extended RetryAfter: %v > %v", r.RetryAfter, first.RetryAfter)
		}
	}
	// And after waiting out the interval, the request passes.
	c.advance(first.RetryAfter)
	if r, _ := l.Allow("k", p); !r.Allowed {
		t.Fatal("attempt after RetryAfter elapsed must pass")
	}
}

func TestSustainedRate_IsHonored(t *testing.T) {
	l, c := newTestLimiter()
	p := Params{Rate: 20, Burst: 1} // one event per 50 ms

	allowed := 0
	for i := 0; i < 200; i++ { // 2 simulated seconds in 10 ms steps
		if r, _ := l.Allow("k", p); r.Allowed {
			allowed++
		}
		c.advance(10 * time.Millisecond)
	}
	// 2 s at 20/s = 40 (±1 boundary effect).
	if allowed < 39 || allowed > 41 {
		t.Fatalf("allowed %d events over 2s at rate 20/s, want ~40", allowed)
	}
}

func TestIdle_RestoresFullBurst(t *testing.T) {
	l, c := newTestLimiter()
	p := Params{Rate: 10, Burst: 5}

	for i := 0; i < 5; i++ {
		l.Allow("k", p) //nolint:errcheck // exhausting the burst
	}
	c.advance(time.Second) // > Burst·T of idle time
	for i := 0; i < 5; i++ {
		if r, _ := l.Allow("k", p); !r.Allowed {
			t.Fatalf("after idle, burst attempt %d denied", i+1)
		}
	}
}

func TestKeys_AreIndependent(t *testing.T) {
	l, _ := newTestLimiter()
	p := Params{Rate: 10, Burst: 1}

	l.Allow("a", p) //nolint:errcheck
	if r, _ := l.Allow("b", p); !r.Allowed {
		t.Fatal("key b throttled by key a's traffic")
	}
}

func TestParams_Validate(t *testing.T) {
	for _, bad := range []Params{{Rate: 0, Burst: 1}, {Rate: -5, Burst: 1}, {Rate: 1, Burst: 0}} {
		if _, err := NewMemoryLimiter().Allow("k", bad); err == nil {
			t.Fatalf("params %+v accepted, want validation error", bad)
		}
	}
}
