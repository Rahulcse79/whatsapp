package domain

import (
	"testing"
	"time"
)

var t0 = time.Unix(1_800_000_000, 0)

const ttl = FloorTTL

func TestAcquire_EmptyFloorGrantsWithFence1(t *testing.T) {
	f := &Floor{}
	r := f.Acquire("a", t0, ttl)
	if r.Granted == nil || r.Granted.Fence != 1 || r.Granted.Device != "a" {
		t.Fatalf("acquire on empty floor = %+v, want granted fence 1", r)
	}
}

func TestAcquire_QueuesWhileHeld_FIFO(t *testing.T) {
	f := &Floor{}
	f.Acquire("a", t0, ttl) // a holds
	if r := f.Acquire("b", t0, ttl); r.Granted != nil || r.Position != 1 {
		t.Fatalf("b = %+v, want queued pos 1", r)
	}
	if r := f.Acquire("c", t0, ttl); r.Position != 2 {
		t.Fatalf("c = %+v, want queued pos 2", r)
	}
	// b re-acquires → same position, idempotent (no duplicate enqueue).
	if r := f.Acquire("b", t0, ttl); r.Position != 1 {
		t.Fatalf("b re-queue = %+v, want pos 1", r)
	}
}

func TestReAcquireOwnFloor_Refreshes(t *testing.T) {
	f := &Floor{}
	g := f.Acquire("a", t0, ttl).Granted
	r := f.Acquire("a", t0.Add(time.Second), ttl)
	if r.Granted == nil || r.Granted.Fence != g.Fence {
		t.Fatalf("re-acquire = %+v, want same fence %d", r, g.Fence)
	}
	if !f.held(t0.Add(2500 * time.Millisecond)) {
		t.Fatal("lease should have been refreshed to now+ttl")
	}
}

func TestHeartbeat(t *testing.T) {
	f := &Floor{}
	f.Acquire("a", t0, ttl)
	if hb := f.Heartbeat("a", t0.Add(time.Second), ttl); !hb.Held || hb.Fence != 1 {
		t.Fatalf("holder heartbeat = %+v, want held fence 1", hb)
	}
	if hb := f.Heartbeat("b", t0, ttl); hb.Held {
		t.Fatal("non-holder heartbeat must not be held")
	}
	// A heartbeat after the lease lapsed is lost.
	if hb := f.Heartbeat("a", t0.Add(3*time.Second), ttl); hb.Held {
		t.Fatal("lapsed heartbeat must be lost")
	}
}

func TestRelease_PromotesQueueHead(t *testing.T) {
	f := &Floor{}
	f.Acquire("a", t0, ttl)
	f.Acquire("b", t0, ttl)
	f.Acquire("c", t0, ttl)

	r := f.Release("a", t0, ttl)
	if !r.Released || r.Next == nil || r.Next.Device != "b" || r.Next.Fence != 2 {
		t.Fatalf("release = %+v, want promote b fence 2", r)
	}
	// c is now head.
	if r := f.Release("b", t0, ttl); r.Next == nil || r.Next.Device != "c" || r.Next.Fence != 3 {
		t.Fatalf("release b = %+v, want promote c fence 3", r)
	}
	// Empty queue → released, no next.
	if r := f.Release("c", t0, ttl); !r.Released || r.Next != nil {
		t.Fatalf("release c = %+v, want released, no next", r)
	}
}

func TestRelease_ByNonHolderIsNoop(t *testing.T) {
	f := &Floor{}
	f.Acquire("a", t0, ttl)
	if r := f.Release("b", t0, ttl); r.Released {
		t.Fatal("release by non-holder must be a no-op")
	}
}

func TestSweep_LapsedHolderDemotedAndHeadPromoted(t *testing.T) {
	f := &Floor{}
	f.Acquire("a", t0, ttl)
	f.Acquire("b", t0, ttl)

	// Before lapse: nothing.
	if s := f.Sweep(t0.Add(time.Second), ttl); s.Demoted != "" || s.Promoted != nil {
		t.Fatalf("early sweep = %+v, want no-op", s)
	}
	// After lapse: a demoted, b promoted with a fresh fence.
	s := f.Sweep(t0.Add(3*time.Second), ttl)
	if s.Demoted != "a" || s.Promoted == nil || s.Promoted.Device != "b" || s.Promoted.Fence != 2 {
		t.Fatalf("lapse sweep = %+v, want demote a + promote b fence 2", s)
	}
}

func TestAcquire_AfterLapseRespectsQueueOrder(t *testing.T) {
	f := &Floor{}
	f.Acquire("a", t0, ttl) // a holds
	f.Acquire("b", t0, ttl) // b queued

	// After a lapses, c acquires: the head (b) is promoted, c queues behind.
	r := f.Acquire("c", t0.Add(3*time.Second), ttl)
	if r.Demoted != "a" {
		t.Fatalf("expected a demoted, got %q", r.Demoted)
	}
	if r.Promoted == nil || r.Promoted.Device != "b" {
		t.Fatalf("expected b promoted, got %+v", r.Promoted)
	}
	if r.Granted != nil || r.Position != 1 {
		t.Fatalf("c = %+v, want queued pos 1 behind b", r)
	}
}

func TestAcquire_AfterLapseEmptyQueueGrantsRequester(t *testing.T) {
	f := &Floor{}
	f.Acquire("a", t0, ttl)
	r := f.Acquire("b", t0.Add(3*time.Second), ttl) // a lapsed, no queue
	if r.Demoted != "a" || r.Granted == nil || r.Granted.Device != "b" {
		t.Fatalf("post-lapse acquire = %+v, want demote a + grant b", r)
	}
}

func TestQueueFull(t *testing.T) {
	f := &Floor{}
	f.Acquire("holder", t0, ttl)
	for i := 0; i < MaxQueue; i++ {
		f.Acquire(string(rune('A'+i%26))+string(rune('0'+i/26)), t0, ttl)
	}
	if r := f.Acquire("overflow", t0, ttl); !r.Full {
		t.Fatalf("acquire past MaxQueue = %+v, want Full", r)
	}
}

func TestFenceIsMonotonic(t *testing.T) {
	f := &Floor{}
	f.Acquire("a", t0, ttl)
	f.Acquire("b", t0, ttl)
	f1 := f.Release("a", t0, ttl).Next.Fence // b granted
	f.Acquire("c", t0, ttl)
	f2 := f.Release("b", t0, ttl).Next.Fence // c granted
	if f1 != 2 || f2 != 3 {
		t.Fatalf("fences = %d,%d, want 2,3 (monotonic)", f1, f2)
	}
}
