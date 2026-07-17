package gateway

import (
	"context"
	"sync"
	"time"
)

// RouteStore is the cross-pod routing table: which gateway pod holds a
// device's connection (Valkey key route:{device_id}, TTL-refreshed). Any pod
// can serve any device, so there is no stickiness — the table is the source
// of truth for delivery (ws-gateway-lld §1).
type RouteStore interface {
	// Claim records podID as the owner of deviceID's route (last-writer-wins:
	// the newest connection owns the device).
	Claim(ctx context.Context, deviceID, podID string, ttl time.Duration) error
	// Refresh extends the TTL only if podID still owns the route. owned=false
	// means another pod took over (a newer connection elsewhere) — the caller
	// closes the stale connection with 4409.
	Refresh(ctx context.Context, deviceID, podID string, ttl time.Duration) (owned bool, err error)
	// Release removes the route only if podID still owns it.
	Release(ctx context.Context, deviceID, podID string) error
}

// MemoryRouteStore is an in-process RouteStore for tests and single-pod dev.
// Production uses the Valkey store (adapters/valkey.go) so routing survives
// across pods.
type MemoryRouteStore struct {
	mu     sync.Mutex
	owners map[string]string // deviceID → podID
}

func NewMemoryRouteStore() *MemoryRouteStore {
	return &MemoryRouteStore{owners: make(map[string]string)}
}

func (m *MemoryRouteStore) Claim(_ context.Context, deviceID, podID string, _ time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.owners[deviceID] = podID
	return nil
}

func (m *MemoryRouteStore) Refresh(_ context.Context, deviceID, podID string, _ time.Duration) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.owners[deviceID] == podID, nil
}

func (m *MemoryRouteStore) Release(_ context.Context, deviceID, podID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.owners[deviceID] == podID {
		delete(m.owners, deviceID)
	}
	return nil
}
