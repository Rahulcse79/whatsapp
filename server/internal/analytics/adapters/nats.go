package adapters

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/nats-io/nats.go"

	"github.com/whatsapp-v2/server/internal/analytics"
	"github.com/whatsapp-v2/server/internal/analytics/domain"
)

// EventSubject is where producers publish metadata events and the consumer
// aggregates them. QueueGroup ensures exactly one pod applies each event (so a
// counter increments once), while the distinct sketch is idempotent regardless.
const (
	EventSubject = "analytics.event"
	QueueGroup   = "analytics"
)

// Publisher implements analytics.Emitter over core NATS. Emission is
// fire-and-forget: analytics must never slow or fail the producing request.
type Publisher struct{ nc *nats.Conn }

func NewPublisher(nc *nats.Conn) *Publisher { return &Publisher{nc: nc} }

var _ analytics.Emitter = (*Publisher)(nil)

func (p *Publisher) Emit(_ context.Context, e analytics.Event) error {
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	return p.nc.Publish(EventSubject, b)
}

// Signup and Active are the producer-side conveniences (e.g. auth). They swallow
// errors — a dropped analytics event is never worth failing a login over.
func (p *Publisher) Signup(ctx context.Context) {
	_ = p.Emit(ctx, analytics.Event{Kind: domain.KindSignup})
}

func (p *Publisher) Active(ctx context.Context, userID string) {
	_ = p.Emit(ctx, analytics.Event{Kind: domain.KindActiveUser, UserID: userID})
}

// Consumer subscribes the analytics subject and feeds each event to the service.
type Consumer struct {
	nc  *nats.Conn
	svc *analytics.Service
	log *slog.Logger
	sub *nats.Subscription
}

func NewConsumer(nc *nats.Conn, svc *analytics.Service, log *slog.Logger) *Consumer {
	return &Consumer{nc: nc, svc: svc, log: log}
}

// Start subscribes in the shared queue group. Bad frames are logged and dropped;
// ingest errors are logged (the event is lost, never retried — analytics is
// best-effort, not the system of record).
func (c *Consumer) Start() error {
	sub, err := c.nc.QueueSubscribe(EventSubject, QueueGroup, func(m *nats.Msg) {
		var e analytics.Event
		if err := json.Unmarshal(m.Data, &e); err != nil {
			c.log.Warn("analytics: bad event frame", "err", err)
			return
		}
		if !e.Kind.Known() {
			return
		}
		if err := c.svc.Ingest(context.Background(), e); err != nil {
			c.log.Warn("analytics: ingest failed", "kind", string(e.Kind), "err", err)
		}
	})
	if err != nil {
		return err
	}
	c.sub = sub
	return nil
}

func (c *Consumer) Stop() {
	if c.sub != nil {
		_ = c.sub.Unsubscribe()
	}
}
