// Package adapters implements PTT's FloorStore over Valkey (Lua, distributed) and
// in memory (single-process: tests + the offline profile). Both delegate to the
// same domain.Floor algorithm so their behavior agrees.
package adapters

import (
	"context"
	"sync"
	"time"

	"github.com/whatsapp-v2/server/internal/ptt/domain"
)

// MemoryFloorStore keeps one domain.Floor per room behind a mutex. Clock is
// injectable for tests (lapse/heartbeat timing).
type MemoryFloorStore struct {
	mu    sync.Mutex
	rooms map[string]*domain.Floor
	Clock func() time.Time
	ttl   time.Duration
}

func NewMemoryFloorStore() *MemoryFloorStore {
	return &MemoryFloorStore{rooms: map[string]*domain.Floor{}, Clock: time.Now, ttl: domain.FloorTTL}
}

func (m *MemoryFloorStore) floor(room string) *domain.Floor {
	f, ok := m.rooms[room]
	if !ok {
		f = &domain.Floor{}
		m.rooms[room] = f
	}
	return f
}

func (m *MemoryFloorStore) Acquire(_ context.Context, room, participant string) (domain.AcquireResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.floor(room).Acquire(participant, m.Clock(), m.ttl), nil
}

func (m *MemoryFloorStore) Heartbeat(_ context.Context, room, participant string) (domain.HeartbeatResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.floor(room).Heartbeat(participant, m.Clock(), m.ttl), nil
}

func (m *MemoryFloorStore) Release(_ context.Context, room, participant string) (domain.ReleaseResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.floor(room).Release(participant, m.Clock(), m.ttl), nil
}

func (m *MemoryFloorStore) Sweep(_ context.Context, room string) (domain.SweepResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.floor(room).Sweep(m.Clock(), m.ttl)
	if m.rooms[room].Idle(m.Clock()) {
		delete(m.rooms, room) // reclaim empty rooms
	}
	return r, nil
}

func (m *MemoryFloorStore) Snapshot(_ context.Context, room string) (string, int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	holder, n := m.floor(room).Snapshot(m.Clock())
	return holder, n, nil
}

func (m *MemoryFloorStore) ActiveRooms(_ context.Context) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.Clock()
	out := make([]string, 0, len(m.rooms))
	for room, f := range m.rooms {
		if !f.Idle(now) {
			out = append(out, room)
		}
	}
	return out, nil
}
