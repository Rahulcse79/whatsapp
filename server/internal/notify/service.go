package notify

import (
	"context"
	"errors"
	"log/slog"
)

// Outcome tells the consumer how to settle a JetStream message.
type Outcome int

const (
	// Ack: done — remove from the stream.
	Ack Outcome = iota
	// Nack: redeliver later (transient failure or an open breaker). The work
	// stays in the stream; nothing is dropped.
	Nack
	// Dead: redeliveries exhausted — park on the DLQ, then ack.
	Dead
)

// Service is the dispatch pipeline.
type Service struct {
	tokens        TokenStore
	drivers       map[Provider]PushDriver
	breakers      map[Provider]*Breaker
	maxDeliveries int
	log           *slog.Logger
}

// NewService wires the pipeline. maxDeliveries is the redelivery ceiling before
// a dispatch is dead-lettered (notification-svc-lld: 5). A breaker is created
// per registered driver.
func NewService(tokens TokenStore, drivers []PushDriver, maxDeliveries int, log *slog.Logger) *Service {
	if maxDeliveries <= 0 {
		maxDeliveries = DefaultThreshold
	}
	dm := make(map[Provider]PushDriver, len(drivers))
	bm := make(map[Provider]*Breaker, len(drivers))
	for _, d := range drivers {
		dm[d.Provider()] = d
		bm[d.Provider()] = NewBreaker(DefaultThreshold, DefaultWindow, DefaultCooldown)
	}
	return &Service{tokens: tokens, drivers: dm, breakers: bm, maxDeliveries: maxDeliveries, log: log}
}

// Handle processes one dispatch and returns how to settle it. deliveries is the
// JetStream NumDelivered count (1 on the first attempt).
//
// Invariant (DONE criterion): a provider outage never drops a push. An open
// breaker or a transient error returns Nack, so the message stays in the
// stream and redelivers; only success, an invalid token, a missing token, or a
// missing driver acks — and exhausted redeliveries go to the DLQ, never /dev/null.
func (s *Service) Handle(ctx context.Context, d Dispatch, deliveries int) Outcome {
	token, provider, err := s.tokens.Resolve(ctx, d.RecipientDeviceID)
	switch {
	case errors.Is(err, ErrNoToken):
		return Ack // nothing to deliver
	case err != nil:
		s.log.Warn("token resolve failed", "device_id", d.RecipientDeviceID, "err", err)
		return s.retryOrDead(deliveries) // transient DB error
	}

	driver := s.drivers[provider]
	breaker := s.breakers[provider]
	if driver == nil || breaker == nil {
		s.log.Warn("no driver for provider", "provider", provider.String())
		return Ack // can't deliver; don't spin forever
	}

	// Breaker open → hold the work in the stream until the provider recovers.
	if !breaker.Allow() {
		return Nack
	}

	err = driver.Send(ctx, token, d.Payload)
	if err == nil {
		breaker.Record(true)
		return Ack
	}

	// A dead token is the client's problem, not a provider outage: prune it and
	// ack, and do NOT trip the breaker.
	if errors.Is(err, ErrTokenInvalid) {
		if derr := s.tokens.Delete(ctx, d.RecipientDeviceID); derr != nil {
			s.log.Warn("deleting invalid token failed", "device_id", d.RecipientDeviceID, "err", derr)
		}
		return Ack
	}

	// Transient / provider failure: count it against the breaker and redeliver.
	breaker.Record(false)
	s.log.Warn("push send failed", "provider", provider.String(), "device_id", d.RecipientDeviceID, "err", err)
	return s.retryOrDead(deliveries)
}

func (s *Service) retryOrDead(deliveries int) Outcome {
	if deliveries >= s.maxDeliveries {
		return Dead
	}
	return Nack
}
