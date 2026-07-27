package presence

import (
	"sync"
	"time"
)

// DefaultGrace is how long an offline announcement is withheld after a
// disconnect, so a brief network flap (disconnect then quick reconnect) never
// shows the user as offline (HLD §8.5).
const DefaultGrace = 15 * time.Second

// FlapDamper delays offline announcements. One pending timer per key
// (conventionally "user:device"): a reconnect within the grace window cancels
// it; a second disconnect replaces it. Safe for concurrent use.
type FlapDamper struct {
	grace  time.Duration
	mu     sync.Mutex
	timers map[string]*time.Timer
}

func NewFlapDamper(grace time.Duration) *FlapDamper {
	if grace <= 0 {
		grace = DefaultGrace
	}
	return &FlapDamper{grace: grace, timers: make(map[string]*time.Timer)}
}

// Disconnected schedules onOffline to run after the grace period, unless
// Reconnected (or another Disconnected) intervenes first. Replaces any pending
// timer for the key.
func (d *FlapDamper) Disconnected(key string, onOffline func()) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if t, ok := d.timers[key]; ok {
		t.Stop()
	}
	d.timers[key] = time.AfterFunc(d.grace, func() {
		d.mu.Lock()
		// Only fire if this timer is still the registered one — guards against
		// a Reconnected that raced past Stop.
		if cur, ok := d.timers[key]; ok && cur != nil {
			delete(d.timers, key)
			d.mu.Unlock()
			onOffline()
			return
		}
		d.mu.Unlock()
	})
}

// Reconnected cancels a pending offline for the key, returning true if one was
// pending (i.e. this was a genuine flap the damper absorbed).
func (d *FlapDamper) Reconnected(key string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if t, ok := d.timers[key]; ok {
		t.Stop()
		delete(d.timers, key)
		return true
	}
	return false
}

// Pending reports whether an offline announcement is currently scheduled.
func (d *FlapDamper) Pending(key string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	_, ok := d.timers[key]
	return ok
}

// StopAll cancels every pending timer (shutdown).
func (d *FlapDamper) StopAll() {
	d.mu.Lock()
	defer d.mu.Unlock()
	for k, t := range d.timers {
		t.Stop()
		delete(d.timers, k)
	}
}
