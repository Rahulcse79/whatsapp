package ptt

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/whatsapp-v2/server/internal/ptt/domain"
)

// Protocol scenario P11 (test-strategy §3): PTT concurrent acquire ×10, speaker
// crash, zombie fence — expecting FIFO fairness, bounded failover, and stale
// audio dead at the SFU. These drive the T3.04 floor control through the service
// (fenced token + FIFO + SFU permission flips); true cross-pod atomicity is
// covered by the Valkey integration test.

// P11a: ten devices contend for the floor. Exactly one wins; the rest queue in
// arrival order, and releasing the holder promotes them FIFO with a monotonic
// fence.
func TestScenarioP11_ConcurrentAcquireFIFOFairness(t *testing.T) {
	h := newHarness()
	ctx := context.Background()

	for i := 0; i < 10; i++ {
		if err := h.svc.Acquire(ctx, "room", ident("u"+strconv.Itoa(i), "d1")); err != nil {
			t.Fatal(err)
		}
	}

	// u0 holds (fence 1); u1..u9 wait at positions 1..9 (FIFO fairness).
	if h.sig.grants["u0:d1"] != 1 {
		t.Fatalf("first acquirer should hold fence 1, grants=%+v", h.sig.grants)
	}
	for i := 1; i < 10; i++ {
		if pos := h.sig.queued["u"+strconv.Itoa(i)+":d1"]; pos != i {
			t.Fatalf("u%d queued at %d, want FIFO position %d", i, pos, i)
		}
	}

	// Each release promotes exactly the next waiter, fence strictly increasing.
	for i := 0; i < 9; i++ {
		if err := h.svc.Release(ctx, "room", ident("u"+strconv.Itoa(i), "d1")); err != nil {
			t.Fatal(err)
		}
		next := "u" + strconv.Itoa(i+1) + ":d1"
		if got := h.sig.grants[next]; got != int64(i+2) {
			t.Fatalf("promoted %s at fence %d, want %d (monotonic)", next, got, i+2)
		}
	}
}

// P11b: the speaker crashes (stops heartbeating). Before the lease lapses the
// floor is untouched; once it lapses the sweep fails over to the next waiter.
func TestScenarioP11_SpeakerCrashFailover(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	_ = h.svc.Acquire(ctx, "room", ident("holder", "d1")) // fence 1
	_ = h.svc.Acquire(ctx, "room", ident("waiter", "d1")) // queued

	// Still within the lease → no failover.
	h.now = h.now.Add(domain.FloorTTL / 2)
	if n, _ := h.svc.SweepAll(ctx); n != 0 {
		t.Fatalf("swept %d before the lease lapsed, want 0", n)
	}

	// Lease lapsed → the waiter takes the floor.
	h.now = h.now.Add(domain.FloorTTL)
	if n, _ := h.svc.SweepAll(ctx); n != 1 {
		t.Fatalf("failover swept %d, want 1", n)
	}
	if h.sig.grants["waiter:d1"] != 2 {
		t.Fatalf("waiter should be promoted with fence 2, grants=%+v", h.sig.grants)
	}
	if !h.sfu.denied("holder:d1") {
		t.Fatal("crashed holder must be muted at the SFU")
	}
}

// P11c: the zombie fence. After a crash+failover the ex-speaker's fence is
// superseded; when it resumes after the partition its heartbeat is lost and its
// publish was already revoked — so its stale RTP is dead at the SFU.
func TestScenarioP11_ZombieFenceStaleAudioDead(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	_ = h.svc.Acquire(ctx, "room", ident("zombie", "d1")) // fence 1
	_ = h.svc.Acquire(ctx, "room", ident("fresh", "d1"))  // queued

	h.now = h.now.Add(domain.FloorTTL + time.Second)
	_, _ = h.svc.SweepAll(ctx) // zombie demoted (muted), fresh promoted

	const zombieFence = int64(1)
	if fresh := h.sig.grants["fresh:d1"]; fresh <= zombieFence {
		t.Fatalf("fresh holder fence %d must supersede the zombie's %d", fresh, zombieFence)
	}
	if !h.sfu.denied("zombie:d1") {
		t.Fatal("zombie was not muted on demotion — stale audio would leak")
	}

	// The zombie resumes: its heartbeat finds it no longer holds the floor.
	if err := h.svc.Heartbeat(ctx, "room", ident("zombie", "d1")); err != nil {
		t.Fatal(err)
	}
	if h.sig.revoked["zombie:d1"] != "lost" {
		t.Fatalf("resumed zombie heartbeat should be lost, revoked=%+v", h.sig.revoked)
	}
}
