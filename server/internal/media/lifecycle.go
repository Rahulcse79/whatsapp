package media

import (
	"context"
	"log/slog"
)

// media.lifecycle event kinds. media-svc PUBLISHES its own facts (uploaded /
// ref_added / ref_removed / orphaned — see adapters/nats.go) and SUBSCRIBES to
// the same subject to apply *inbound* dereference commands from core-api (a
// delete-for-everyone that removed media, or an account purge; media-svc-lld §4).
//
// The consumer acts ONLY on `dereference`. Ignoring media-svc's own facts is
// what stops the publish↔subscribe loop from recursing: DecRef emits ref_removed,
// which the consumer must not treat as another command.
const (
	LifecycleDereference = "dereference" // inbound command: drop one media reference

	// media-svc's own published facts — ignored on consume.
	LifecycleUploaded   = "uploaded"
	LifecycleRefAdded   = "ref_added"
	LifecycleRefRemoved = "ref_removed"
	LifecycleOrphaned   = "orphaned"
)

// LifecycleEvent is a parsed media.lifecycle payload (JSON today; proto
// events.v1.MediaLifecycle once generated code is committed to the build).
type LifecycleEvent struct {
	Kind      string
	ObjectKey string
	EventID   string // idempotency key for at-least-once dedupe
}

// Dereferencer is the slice of Service the consumer needs: the refcount decrement.
type Dereferencer interface {
	DecRef(ctx context.Context, objectKey string) error
}

// Deduper records processed event ids so a redelivered event is a no-op. Optional
// (nil ⇒ no dedupe — acceptable on core NATS, which is at-most-once today).
type Deduper interface {
	// Seen atomically marks eventID processed and reports whether it already was.
	Seen(ctx context.Context, eventID string) (bool, error)
}

// LifecycleConsumer applies inbound media.lifecycle dereference commands.
type LifecycleConsumer struct {
	refs   Dereferencer
	dedupe Deduper
	log    *slog.Logger
}

func NewLifecycleConsumer(refs Dereferencer, dedupe Deduper, log *slog.Logger) *LifecycleConsumer {
	return &LifecycleConsumer{refs: refs, dedupe: dedupe, log: log}
}

// Handle applies one event. It returns an error ONLY for a transient failure
// (the caller may let the event be redelivered); an ignorable or malformed event
// is a nil no-op. With a Deduper configured and an event id present, a duplicate
// delivery is dropped so the decrement is applied at most once.
func (c *LifecycleConsumer) Handle(ctx context.Context, ev LifecycleEvent) error {
	if ev.Kind != LifecycleDereference {
		return nil // our own facts / unknown kinds — ignore (no pub↔sub loop)
	}
	if ev.ObjectKey == "" {
		c.log.Warn("media lifecycle: dereference with empty object_key")
		return nil
	}
	if c.dedupe != nil && ev.EventID != "" {
		seen, err := c.dedupe.Seen(ctx, ev.EventID)
		if err != nil {
			return err // dedupe backend is transient → let it be retried
		}
		if seen {
			return nil // already applied
		}
	}
	// DecRef is itself idempotent against a missing object (ErrNotFound → nil).
	return c.refs.DecRef(ctx, ev.ObjectKey)
}

var _ Dereferencer = (*Service)(nil)
