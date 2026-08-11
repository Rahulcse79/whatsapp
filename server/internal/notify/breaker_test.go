package notify

import (
	"testing"
	"time"
)

func newTestBreaker() (*Breaker, *int64) {
	nowNS := time.Unix(1_700_000_000, 0).UnixNano()
	b := NewBreaker(3, 30*time.Second, 60*time.Second)
	b.now = func() time.Time { return time.Unix(0, nowNS) }
	return b, &nowNS
}

func advance(clock *int64, d time.Duration) { *clock += int64(d) }

func TestBreaker_OpensAfterThreshold(t *testing.T) {
	b, _ := newTestBreaker()
	for i := 0; i < 2; i++ {
		if !b.Allow() {
			t.Fatalf("attempt %d blocked before threshold", i)
		}
		b.Record(false)
	}
	// Third failure trips it.
	if !b.Allow() {
		t.Fatal("third attempt should still be allowed (breaker not yet open)")
	}
	b.Record(false)
	if b.Allow() {
		t.Fatal("breaker should be OPEN after the threshold of failures")
	}
	if !b.Open() {
		t.Fatal("Open() should report true")
	}
}

func TestBreaker_HalfOpenProbeThenClose(t *testing.T) {
	b, clock := newTestBreaker()
	for i := 0; i < 3; i++ {
		b.Allow()
		b.Record(false)
	}
	if b.Allow() {
		t.Fatal("should be open")
	}
	// Still open before cooldown.
	advance(clock, 59*time.Second)
	if b.Allow() {
		t.Fatal("still within cooldown — must stay closed to traffic")
	}
	// After cooldown, exactly one probe is admitted.
	advance(clock, 2*time.Second)
	if !b.Allow() {
		t.Fatal("cooldown elapsed — one probe should be admitted (half-open)")
	}
	if b.Allow() {
		t.Fatal("half-open must admit only a single probe")
	}
	// Probe succeeds → closed.
	b.Record(true)
	if !b.Allow() {
		t.Fatal("successful probe should close the breaker")
	}
}

func TestBreaker_HalfOpenProbeFailReopens(t *testing.T) {
	b, clock := newTestBreaker()
	for i := 0; i < 3; i++ {
		b.Allow()
		b.Record(false)
	}
	advance(clock, 61*time.Second)
	if !b.Allow() {
		t.Fatal("probe should be admitted after cooldown")
	}
	b.Record(false) // probe fails
	if b.Allow() {
		t.Fatal("failed probe should re-open the breaker")
	}
}

func TestBreaker_FailuresOutsideWindowDontAccumulate(t *testing.T) {
	b, clock := newTestBreaker()
	b.Allow()
	b.Record(false)
	advance(clock, 31*time.Second) // first failure ages out of the window
	b.Allow()
	b.Record(false)
	b.Allow()
	b.Record(false)
	// Only 2 failures within the window → still closed.
	if !b.Allow() {
		t.Fatal("failures outside the window should not open the breaker")
	}
}
