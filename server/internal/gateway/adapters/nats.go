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

// NATSReceiptSource subscribes to per-device receipt subjects
// (dev.{id}.receipt, published by chat/adapters.RelayReceipt). Same
// live-only semantics as delivery: receipts are lossy conveniences, so a
// receipt published while the device is offline is simply gone — the message
// info screen reconciles from peer state on demand (T1+).
type NATSReceiptSource struct{ nc *nats.Conn }

func NewNATSReceiptSource(nc *nats.Conn) *NATSReceiptSource {
	return &NATSReceiptSource{nc: nc}
}

func (s *NATSReceiptSource) Subscribe(deviceID string, deliver func([]byte)) (func(), error) {
	sub, err := s.nc.Subscribe("dev."+deviceID+".receipt", func(msg *nats.Msg) {
		deliver(msg.Data)
	})
	if err != nil {
		return nil, fmt.Errorf("subscribing receipts for %s: %w", deviceID, err)
	}
	return func() { _ = sub.Unsubscribe() }, nil
}

// NATSCallSource subscribes to per-device call-signaling subjects
// (dev.{id}.call, published by calls/adapters.Signaler). Same live-only,
// lossy semantics as receipts: a ring frame published while the device is
// between subscriptions is recovered by the ring state machine (re-ring /
// timeout), never replayed. Mirrors calls/adapters.CallSubject.
type NATSCallSource struct{ nc *nats.Conn }

func NewNATSCallSource(nc *nats.Conn) *NATSCallSource {
	return &NATSCallSource{nc: nc}
}

func (s *NATSCallSource) Subscribe(deviceID string, deliver func([]byte)) (func(), error) {
	sub, err := s.nc.Subscribe("dev."+deviceID+".call", func(msg *nats.Msg) {
		deliver(msg.Data)
	})
	if err != nil {
		return nil, fmt.Errorf("subscribing calls for %s: %w", deviceID, err)
	}
	return func() { _ = sub.Unsubscribe() }, nil
}
