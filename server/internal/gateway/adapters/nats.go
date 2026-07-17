package adapters

import (
	"fmt"

	"github.com/nats-io/nats.go"
)

// NATSDeliverySource subscribes to per-device delivery subjects over core
// NATS. Live-path only: durability is the PG inbox + resume replay, so a
// message published while the device is between subscriptions is simply
// picked up by replay (internal-events-nats.md §1 documents the divergence
// from the JetStream DELIVERY stream).
type NATSDeliverySource struct{ nc *nats.Conn }

func NewNATSDeliverySource(nc *nats.Conn) *NATSDeliverySource {
	return &NATSDeliverySource{nc: nc}
}

func (s *NATSDeliverySource) Subscribe(deviceID string, deliver func([]byte)) (func(), error) {
	sub, err := s.nc.Subscribe("dev."+deviceID+".out", func(msg *nats.Msg) {
		deliver(msg.Data)
	})
	if err != nil {
		return nil, fmt.Errorf("subscribing delivery for %s: %w", deviceID, err)
	}
	return func() { _ = sub.Unsubscribe() }, nil
}
