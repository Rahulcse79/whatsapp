package adapters

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/nats-io/nats.go"
)

// userEventsSubject is where device lifecycle facts are published
// (internal-events-nats.md: user.events on the DOMAIN stream).
const userEventsSubject = "user.events"

// NATSEvents publishes device lifecycle events. Best-effort: a publish
// failure is logged, never propagated — revocation already committed to
// PostgreSQL (the source of truth), and consumers reconcile.
//
// NOTE: this uses core NATS publish. Binding the DOMAIN JetStream stream and
// its durable consumers is wired with the events infra (T0.16/T0.23); the
// proto payloads (events.v1.UserEvent) replace this JSON once generated code
// is committed to the build.
type NATSEvents struct {
	nc  *nats.Conn
	log *slog.Logger
}

func NewNATSEvents(nc *nats.Conn, log *slog.Logger) *NATSEvents {
	return &NATSEvents{nc: nc, log: log}
}

func (e *NATSEvents) publish(kind, userID, deviceID string) {
	payload, _ := json.Marshal(map[string]string{
		"kind": kind, "user_id": userID, "device_id": deviceID,
	})
	if err := e.nc.Publish(userEventsSubject, payload); err != nil {
		e.log.Warn("publishing user event failed (best-effort)",
			"kind", kind, "device_id", deviceID, "err", err)
	}
}

func (e *NATSEvents) DeviceAdded(_ context.Context, userID, deviceID string) {
	e.publish("device_added", userID, deviceID)
}

func (e *NATSEvents) DeviceRevoked(_ context.Context, userID, deviceID string) {
	e.publish("device_revoked", userID, deviceID)
}

// NoopEvents drops events — used in tests and until NATS is wired into a
// running deployable.
type NoopEvents struct{}

func (NoopEvents) DeviceAdded(context.Context, string, string)   {}
func (NoopEvents) DeviceRevoked(context.Context, string, string) {}
