package id

import (
	"testing"
	"time"
)

func TestNew_IsV7AndParses(t *testing.T) {
	s := New()
	u, err := Parse(s)
	if err != nil {
		t.Fatalf("Parse(New()) failed: %v", err)
	}
	if u.Version() != 7 {
		t.Fatalf("version = %d, want 7", u.Version())
	}
}

func TestParse_RejectsV4(t *testing.T) {
	// A well-formed UUIDv4 must be rejected: clients may only send v7.
	const v4 = "f47ac10b-58cc-4372-a567-0e02b2c3d479"
	if _, err := Parse(v4); err == nil {
		t.Fatal("Parse accepted a UUIDv4")
	}
}

func TestParse_RejectsGarbage(t *testing.T) {
	if _, err := Parse("not-a-uuid"); err == nil {
		t.Fatal("Parse accepted garbage")
	}
}

func TestTimestamps_NonDecreasing(t *testing.T) {
	// The timestamp prefix is what buys index locality: across sequential
	// generation the embedded times must never go backwards.
	prev := time.Time{}
	for i := 0; i < 1000; i++ {
		ts := TimeOf(NewUUID())
		if ts.Before(prev) {
			t.Fatalf("timestamp went backwards at i=%d: %v < %v", i, ts, prev)
		}
		prev = ts
	}
}

func TestTimeOf_IsNow(t *testing.T) {
	got := TimeOf(NewUUID())
	if d := time.Since(got); d < -5*time.Second || d > 5*time.Second {
		t.Fatalf("embedded timestamp %v is not close to now", got)
	}
}
