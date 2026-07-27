package presence

import (
	"sync/atomic"
	"testing"
	"time"
)

// A disconnect followed by a quick reconnect within the grace window must NOT
// announce offline — the flap-damping guarantee (HLD §8.5).
func TestFlapDamper_ReconnectWithinGraceSuppressesOffline(t *testing.T) {
	d := NewFlapDamper(60 * time.Millisecond)
	var offline atomic.Int64
	d.Disconnected("u:dev", func() { offline.Add(1) })
	if !d.Pending("u:dev") {
		t.Fatal("offline should be pending after disconnect")
	}
	// Reconnect promptly.
	if !d.Reconnected("u:dev") {
		t.Fatal("Reconnected should report it cancelled a pending offline")
	}
	time.Sleep(120 * time.Millisecond)
	if offline.Load() != 0 {
		t.Fatalf("offline fired %d times despite a flap; want 0", offline.Load())
	}
}

// A disconnect with no reconnect announces offline exactly once after grace.
func TestFlapDamper_OfflineAfterGrace(t *testing.T) {
	d := NewFlapDamper(40 * time.Millisecond)
	var offline atomic.Int64
	d.Disconnected("u:dev", func() { offline.Add(1) })
	time.Sleep(120 * time.Millisecond)
	if got := offline.Load(); got != 1 {
		t.Fatalf("offline fired %d times, want exactly 1", got)
	}
	if d.Pending("u:dev") {
		t.Fatal("timer should be cleared after firing")
	}
}

// A second disconnect replaces the first timer (no double-fire).
func TestFlapDamper_SecondDisconnectReplaces(t *testing.T) {
	d := NewFlapDamper(40 * time.Millisecond)
	var offline atomic.Int64
	d.Disconnected("u:dev", func() { offline.Add(1) })
	time.Sleep(15 * time.Millisecond)
	d.Disconnected("u:dev", func() { offline.Add(1) }) // replace
	time.Sleep(120 * time.Millisecond)
	if got := offline.Load(); got != 1 {
		t.Fatalf("offline fired %d times, want 1 (replacement, not double)", got)
	}
}

func TestFlapDamper_ReconnectWithNothingPending(t *testing.T) {
	d := NewFlapDamper(40 * time.Millisecond)
	if d.Reconnected("nope") {
		t.Fatal("Reconnected should report false when nothing was pending")
	}
}
