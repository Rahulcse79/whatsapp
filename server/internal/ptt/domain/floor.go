// Package domain holds PTT's pure floor-control algorithm: a fenced token + FIFO
// queue (data-structures-algorithms §8). No I/O — this is the single source of
// truth for the state machine; the in-memory store wraps it for tests/offline,
// and the Valkey Lua script (adapters) reproduces it for the distributed case.
//
// Fencing (Kleppmann, applied to media): every grant carries a monotonic fence;
// the SFU is told to allow publish only for the current fence, so a zombie
// ex-speaker resuming after a partition holds a stale fence and is already muted.
package domain

import "time"

const (
	// MaxSpeakMS caps one speaker's turn (calls-ptt-api.md).
	MaxSpeakMS = 60_000
	// FloorTTL is the lease: a heartbeat every 500 ms refreshes it; ~2 missed
	// heartbeats lapse the floor (DS&A §8).
	FloorTTL = 2 * time.Second
	// MaxQueue bounds waiters per room.
	MaxQueue = 500
)

/** Grant names a device that now holds the floor, with its fence. */
type Grant struct {
	Device string
	Fence  int64
}

// AcquireResult is the outcome of an acquire request. Granted is set when the
// requester took (or refreshed) the floor; Position>0 when it queued. Promoted /
// Demoted describe secondary effects the caller must apply to the SFU: Promoted
// is a queue head that took a freed floor, Demoted is a lapsed holder to mute.
type AcquireResult struct {
	Granted  *Grant
	Position int
	Full     bool
	Promoted *Grant
	Demoted  string
}

type HeartbeatResult struct {
	Held  bool
	Fence int64
}

type ReleaseResult struct {
	Released bool
	Next     *Grant // the promoted queue head, if any
}

type SweepResult struct {
	Demoted  string
	Promoted *Grant
}

// Floor is one room's floor state.
type Floor struct {
	holder   string
	fence    int64
	expiry   time.Time
	queue    []string
	fenceSeq int64
}

func (f *Floor) held(now time.Time) bool { return f.holder != "" && now.Before(f.expiry) }

// evictIfLapsed clears an expired holder and returns it (for muting).
func (f *Floor) evictIfLapsed(now time.Time) string {
	if f.holder != "" && !now.Before(f.expiry) {
		d := f.holder
		f.holder, f.fence = "", 0
		return d
	}
	return ""
}

func (f *Floor) grantTo(device string, now time.Time, ttl time.Duration) *Grant {
	f.fenceSeq++
	f.holder, f.fence, f.expiry = device, f.fenceSeq, now.Add(ttl)
	return &Grant{Device: device, Fence: f.fence}
}

func (f *Floor) queueIndex(device string) int {
	for i, d := range f.queue {
		if d == device {
			return i
		}
	}
	return -1
}

func (f *Floor) popHead() string {
	head := f.queue[0]
	f.queue = f.queue[1:]
	return head
}

// Acquire requests the floor for device.
func (f *Floor) Acquire(device string, now time.Time, ttl time.Duration) AcquireResult {
	demoted := f.evictIfLapsed(now)

	if f.holder == "" {
		// Free floor. FIFO: if someone else is at the head of the queue, they get
		// it first and the requester queues behind them.
		if len(f.queue) > 0 && f.queue[0] != device {
			promoted := f.grantTo(f.popHead(), now, ttl)
			pos := f.enqueue(device)
			return AcquireResult{Position: pos, Promoted: promoted, Demoted: demoted}
		}
		if len(f.queue) > 0 && f.queue[0] == device {
			f.popHead()
		}
		return AcquireResult{Granted: f.grantTo(device, now, ttl), Demoted: demoted}
	}

	if f.holder == device {
		// Re-acquire own floor → refresh the lease.
		f.expiry = now.Add(ttl)
		return AcquireResult{Granted: &Grant{Device: device, Fence: f.fence}}
	}

	// Held by someone else → queue (idempotent) and report position.
	if i := f.queueIndex(device); i >= 0 {
		return AcquireResult{Position: i + 1, Demoted: demoted}
	}
	if len(f.queue) >= MaxQueue {
		return AcquireResult{Full: true, Demoted: demoted}
	}
	return AcquireResult{Position: f.enqueue(device), Demoted: demoted}
}

func (f *Floor) enqueue(device string) int {
	if i := f.queueIndex(device); i >= 0 {
		return i + 1
	}
	f.queue = append(f.queue, device)
	return len(f.queue)
}

// Heartbeat refreshes the lease if device still holds the floor.
func (f *Floor) Heartbeat(device string, now time.Time, ttl time.Duration) HeartbeatResult {
	if f.holder == device && f.held(now) {
		f.expiry = now.Add(ttl)
		return HeartbeatResult{Held: true, Fence: f.fence}
	}
	return HeartbeatResult{Held: false}
}

// Release drops device's floor and promotes the next waiter.
func (f *Floor) Release(device string, now time.Time, ttl time.Duration) ReleaseResult {
	if f.holder != device {
		return ReleaseResult{Released: false}
	}
	f.holder, f.fence = "", 0
	if len(f.queue) > 0 {
		return ReleaseResult{Released: true, Next: f.grantTo(f.popHead(), now, ttl)}
	}
	return ReleaseResult{Released: true}
}

// Sweep evicts a lapsed holder and promotes the queue head. Run periodically so
// a waiter still gets the floor when the holder stops heartbeating and no one
// else acquires.
func (f *Floor) Sweep(now time.Time, ttl time.Duration) SweepResult {
	demoted := f.evictIfLapsed(now)
	if f.holder == "" && len(f.queue) > 0 {
		return SweepResult{Demoted: demoted, Promoted: f.grantTo(f.popHead(), now, ttl)}
	}
	return SweepResult{Demoted: demoted}
}

// Idle reports whether the floor is unheld with no waiters (safe to drop).
func (f *Floor) Idle(now time.Time) bool {
	return !f.held(now) && len(f.queue) == 0
}

// Snapshot returns the current holder (empty if unheld/lapsed) and the number of
// waiters — the join response's current_speaker + queue_len.
func (f *Floor) Snapshot(now time.Time) (holder string, queueLen int) {
	if f.held(now) {
		holder = f.holder
	}
	return holder, len(f.queue)
}
