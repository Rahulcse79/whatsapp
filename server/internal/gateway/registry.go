// Package gateway is the stateless WebSocket connection tier (ws-gateway):
// connection registry, JWT-authenticated handshake, route claiming, and
// heartbeats. It holds no business logic and touches no database — a smart
// pipe (Docs/05-services/ws-gateway-lld.md §1).
//
// NOTE: the handshake/heartbeat here use an interim JSON frame codec
// (wire.go). The binary protobuf frame protocol (websocket-protocol.md) is
// wired when the delivery path needs the full frame set (T0.12); the
// registry, route store, and close-code logic below are encoding-agnostic
// and permanent.
package gateway

import (
	"hash/fnv"
	"sync"
)

// shardCount buckets connections to spread lock contention across many small
// mutexes (DS&A §5). 256 keeps per-shard maps tiny at 20k conns/pod.
const shardCount = 256

type shard struct {
	mu    sync.Mutex
	conns map[string]*Conn
}

// Registry maps device_id → the single live connection for that device on
// this pod. It is safe for concurrent use.
type Registry struct {
	shards [shardCount]*shard
}

// NewRegistry builds an empty registry.
func NewRegistry() *Registry {
	r := &Registry{}
	for i := range r.shards {
		r.shards[i] = &shard{conns: make(map[string]*Conn)}
	}
	return r
}

func (r *Registry) shardFor(deviceID string) *shard {
	h := fnv.New32a()
	_, _ = h.Write([]byte(deviceID))
	return r.shards[h.Sum32()%shardCount]
}

// Add registers c for its device and returns the previous connection if one
// existed (which the caller must close with 4409 — a newer connection wins).
func (r *Registry) Add(c *Conn) (displaced *Conn) {
	s := r.shardFor(c.deviceID)
	s.mu.Lock()
	defer s.mu.Unlock()
	old := s.conns[c.deviceID]
	s.conns[c.deviceID] = c
	if old == c {
		return nil
	}
	return old
}

// Remove deletes c from the registry only if it is still the current
// connection for its device, returning true when it removed. This guard is
// what stops a displaced connection's cleanup from evicting the newer one.
func (r *Registry) Remove(c *Conn) bool {
	s := r.shardFor(c.deviceID)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conns[c.deviceID] != c {
		return false
	}
	delete(s.conns, c.deviceID)
	return true
}

// Get returns the live connection for a device, if any.
func (r *Registry) Get(deviceID string) (*Conn, bool) {
	s := r.shardFor(deviceID)
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.conns[deviceID]
	return c, ok
}

// Count returns the total number of live connections (observability).
func (r *Registry) Count() int {
	n := 0
	for _, s := range r.shards {
		s.mu.Lock()
		n += len(s.conns)
		s.mu.Unlock()
	}
	return n
}

// CloseAll closes every connection with the given code — used to drain on
// shutdown. It snapshots per shard so callbacks can't deadlock on the shard
// lock while closing.
func (r *Registry) CloseAll(code CloseCode, reason string) {
	for _, s := range r.shards {
		s.mu.Lock()
		conns := make([]*Conn, 0, len(s.conns))
		for _, c := range s.conns {
			conns = append(conns, c)
		}
		s.mu.Unlock()
		for _, c := range conns {
			c.Close(code, reason)
		}
	}
}
