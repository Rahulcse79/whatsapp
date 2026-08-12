package adapters

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"

	"github.com/whatsapp-v2/server/internal/media"
)

// mediaLifecycleQueue is the NATS queue group for the media.lifecycle consumer.
// Core NATS fans a subject out to EVERY plain subscriber, so with ×2 media-svc
// pods a plain Subscribe would decrement each refcount twice. A queue group makes
// exactly one pod in the group handle each message.
const mediaLifecycleQueue = "media-svc-lifecycle"

// LifecycleSubscriber wires media-svc onto media.lifecycle to apply inbound
// dereference commands (media-svc-lld §4). Core NATS pub/sub (queue-grouped),
// matching the publisher side (NATSEvents) and the current transport reality:
// the DOMAIN JetStream stream lands later; PG refcount is the truth, and a
// decrement missed while no subscriber is up is caught by the account-purge/GC
// backstops.
type LifecycleSubscriber struct {
	nc       *nats.Conn
	consumer *media.LifecycleConsumer
	log      *slog.Logger
}

func NewLifecycleSubscriber(nc *nats.Conn, consumer *media.LifecycleConsumer, log *slog.Logger) *LifecycleSubscriber {
	return &LifecycleSubscriber{nc: nc, consumer: consumer, log: log}
}

type lifecyclePayload struct {
	Kind      string `json:"kind"`
	ObjectKey string `json:"object_key"`
	EventID   string `json:"event_id"`
}

// Start subscribes to media.lifecycle (queue-grouped) and returns an
// unsubscribe func.
func (s *LifecycleSubscriber) Start() (func(), error) {
	sub, err := s.nc.QueueSubscribe(mediaLifecycleSubject, mediaLifecycleQueue, func(m *nats.Msg) {
		var p lifecyclePayload
		if err := json.Unmarshal(m.Data, &p); err != nil {
			s.log.Warn("media lifecycle: undecodable payload", "err", err)
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.consumer.Handle(ctx, media.LifecycleEvent{Kind: p.Kind, ObjectKey: p.ObjectKey, EventID: p.EventID}); err != nil {
			// Core NATS won't redeliver — log for the runbook; the decrement is
			// reconciled by the GC/account-purge backstop.
			s.log.Warn("media lifecycle: handle failed", "kind", p.Kind, "key", p.ObjectKey, "err", err)
		}
	})
	if err != nil {
		return nil, err
	}
	return func() { _ = sub.Unsubscribe() }, nil
}

// LifecycleDeduper records processed media.lifecycle event ids (SET NX with a
// TTL) so a duplicate delivery does not double-decrement a refcount.
type LifecycleDeduper struct {
	client *redis.Client
	ttl    time.Duration
}

func NewLifecycleDeduper(client *redis.Client) *LifecycleDeduper {
	return &LifecycleDeduper{client: client, ttl: 24 * time.Hour}
}

func (d *LifecycleDeduper) Seen(ctx context.Context, eventID string) (bool, error) {
	// SET NX → ok=true means WE set it (first time). Already present ⇒ seen.
	ok, err := d.client.SetNX(ctx, "media_deref:"+eventID, 1, d.ttl).Result()
	if err != nil {
		return false, err
	}
	return !ok, nil
}

var _ media.Deduper = (*LifecycleDeduper)(nil)
