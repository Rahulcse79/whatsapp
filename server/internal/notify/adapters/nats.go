package adapters

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"google.golang.org/protobuf/proto"

	"github.com/whatsapp-v2/server/internal/notify"
	eventsv1 "github.com/whatsapp-v2/server/internal/proto/gen/whatsapp/events/v1"
)

const (
	pushStream  = "PUSH"
	pushSubject = "push.dispatch"
	dlqSubject  = "push.dlq"
	durableName = "notify"
)

// EnsurePushStream creates/updates the PUSH JetStream stream — the first
// durable stream in the system (push genuinely needs redelivery, unlike live
// message delivery which heals via inbox replay; internal-events-nats.md §1).
// It carries the work subject (push.dispatch) and the dead-letter subject
// (push.dlq), 24 h retention.
func EnsurePushStream(ctx context.Context, js jetstream.JetStream) error {
	_, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:      pushStream,
		Subjects:  []string{pushSubject, dlqSubject},
		Retention: jetstream.LimitsPolicy,
		MaxAge:    24 * time.Hour,
	})
	if err != nil {
		return fmt.Errorf("ensuring PUSH stream: %w", err)
	}
	return nil
}

// DLQPublisher parks an exhausted dispatch on push.dlq (durable, inspected by
// runbook) — never silently dropped.
type DLQPublisher struct{ js jetstream.JetStream }

func NewDLQPublisher(js jetstream.JetStream) *DLQPublisher { return &DLQPublisher{js: js} }

func (p *DLQPublisher) PublishDLQ(ctx context.Context, d notify.Dispatch) error {
	payload, err := proto.Marshal(&eventsv1.PushDispatch{
		RecipientDeviceId: d.RecipientDeviceID,
		Kind:              eventsv1.PushKind(d.Payload.Kind),
		CollapseKey:       d.Payload.CollapseKey,
		RingId:            d.Payload.RingID,
	})
	if err != nil {
		return fmt.Errorf("encoding dlq dispatch: %w", err)
	}
	if _, err := p.js.Publish(ctx, dlqSubject, payload); err != nil {
		return fmt.Errorf("publishing to dlq: %w", err)
	}
	return nil
}

var _ notify.DLQPublisher = (*DLQPublisher)(nil)

// Consumer runs the push.dispatch durable pull consumer, settling each message
// per the service's Outcome.
type Consumer struct {
	js         jetstream.JetStream
	svc        *notify.Service
	dlq        notify.DLQPublisher
	maxDeliver int
	log        *slog.Logger
}

func NewConsumer(js jetstream.JetStream, svc *notify.Service, dlq notify.DLQPublisher, maxDeliver int, log *slog.Logger) *Consumer {
	if maxDeliver <= 0 {
		maxDeliver = 6
	}
	return &Consumer{js: js, svc: svc, dlq: dlq, maxDeliver: maxDeliver, log: log}
}

// Run creates the durable consumer and begins consuming; the returned func
// stops consumption (drain on shutdown).
func (c *Consumer) Run(ctx context.Context) (func(), error) {
	cons, err := c.js.CreateOrUpdateConsumer(ctx, pushStream, jetstream.ConsumerConfig{
		Durable:       durableName,
		AckPolicy:     jetstream.AckExplicitPolicy,
		FilterSubject: pushSubject, // never consume push.dlq
		MaxDeliver:    c.maxDeliver,
		AckWait:       30 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("creating push consumer: %w", err)
	}
	cc, err := cons.Consume(func(msg jetstream.Msg) { c.handle(ctx, msg) })
	if err != nil {
		return nil, fmt.Errorf("starting push consumer: %w", err)
	}
	return cc.Stop, nil
}

func (c *Consumer) handle(ctx context.Context, msg jetstream.Msg) {
	var pd eventsv1.PushDispatch
	if err := proto.Unmarshal(msg.Data(), &pd); err != nil {
		c.log.Error("undecodable push dispatch — terminating", "err", err)
		_ = msg.Term() // poison message: neither retry nor DLQ (it's malformed)
		return
	}
	deliveries := 1
	if meta, err := msg.Metadata(); err == nil {
		deliveries = int(meta.NumDelivered)
	}
	d := notify.Dispatch{
		RecipientDeviceID: pd.GetRecipientDeviceId(),
		Payload: notify.Payload{
			Kind:        notify.Kind(pd.GetKind()),
			CollapseKey: pd.GetCollapseKey(),
			RingID:      pd.GetRingId(),
		},
	}
	switch c.svc.Handle(ctx, d, deliveries) {
	case notify.Ack:
		_ = msg.Ack()
	case notify.Nack:
		_ = msg.Nak()
	case notify.Dead:
		if err := c.dlq.PublishDLQ(ctx, d); err != nil {
			// DLQ write failed: nak so the work is retried, never lost.
			c.log.Error("dlq publish failed — nacking", "err", err)
			_ = msg.Nak()
			return
		}
		_ = msg.Ack()
	}
}
