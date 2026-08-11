package adapters

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go"
)

// mediaLifecycleSubject carries refcount + GC facts (internal-events-nats.md:
// media.lifecycle on the DOMAIN stream).
const mediaLifecycleSubject = "media.lifecycle"

// NATSEvents publishes media.lifecycle events. Best-effort core NATS publish —
// the refcount mutation already committed to PostgreSQL (the source of truth).
// Proto payloads (events.v1.MediaLifecycle) replace this JSON once generated
// code is committed to the build.
type NATSEvents struct {
	nc  *nats.Conn
	log *slog.Logger
}

func NewNATSEvents(nc *nats.Conn, log *slog.Logger) *NATSEvents {
	return &NATSEvents{nc: nc, log: log}
}

func (e *NATSEvents) publish(kind, objectKey string, refcount int32) {
	payload, _ := json.Marshal(map[string]any{
		"kind": kind, "object_key": objectKey, "refcount": refcount, "occurred_at_ms": time.Now().UnixMilli(),
	})
	if err := e.nc.Publish(mediaLifecycleSubject, payload); err != nil {
		e.log.Warn("publishing media lifecycle event failed (best-effort)", "kind", kind, "key", objectKey, "err", err)
	}
}

func (e *NATSEvents) Uploaded(_ context.Context, objectKey string) {
	e.publish("uploaded", objectKey, 0)
}
func (e *NATSEvents) RefAdded(_ context.Context, objectKey string, refcount int32) {
	e.publish("ref_added", objectKey, refcount)
}
func (e *NATSEvents) RefRemoved(_ context.Context, objectKey string, refcount int32) {
	e.publish("ref_removed", objectKey, refcount)
}
func (e *NATSEvents) Orphaned(_ context.Context, objectKey string) {
	e.publish("orphaned", objectKey, 0)
}

// NoopEvents drops events (tests / until NATS is wired).
type NoopEvents struct{}

func (NoopEvents) Uploaded(context.Context, string)          {}
func (NoopEvents) RefAdded(context.Context, string, int32)   {}
func (NoopEvents) RefRemoved(context.Context, string, int32) {}
func (NoopEvents) Orphaned(context.Context, string)          {}
