package gateway

import "sync"

// DeliverySource subscribes to a device's live-delivery feed
// (dev.{device_id}.out). The gateway attaches one subscription per
// connection and detaches it on disconnect; missed messages are not its
// problem — the inbox + resume replay own durability.
type DeliverySource interface {
	// Subscribe registers deliver for the device's payloads and returns an
	// unsubscribe function. deliver must not block (it enqueues to the
	// connection's bounded outbound queue).
	Subscribe(deviceID string, deliver func(payload []byte)) (unsubscribe func(), err error)
}

// MemoryDeliverySource is an in-process DeliverySource for tests and
// single-pod dev. Production uses the NATS source (adapters/nats.go).
type MemoryDeliverySource struct {
	mu   sync.Mutex
	subs map[string]func([]byte)
}

func NewMemoryDeliverySource() *MemoryDeliverySource {
	return &MemoryDeliverySource{subs: make(map[string]func([]byte))}
}

func (m *MemoryDeliverySource) Subscribe(deviceID string, deliver func([]byte)) (func(), error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.subs[deviceID] = deliver
	return func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		delete(m.subs, deviceID)
	}, nil
}

// Publish drives tests: it invokes the device's handler if subscribed.
func (m *MemoryDeliverySource) Publish(deviceID string, payload []byte) {
	m.mu.Lock()
	deliver := m.subs[deviceID]
	m.mu.Unlock()
	if deliver != nil {
		deliver(payload)
	}
}
