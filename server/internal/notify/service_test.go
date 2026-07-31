package notify

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
)

// fakeTokens serves a single device's token; supports "no token" and records
// deletes.
type fakeTokens struct {
	mu       sync.Mutex
	token    string
	provider Provider
	noToken  bool
	deleted  int
}

func (f *fakeTokens) Resolve(context.Context, string) (string, Provider, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.noToken {
		return "", 0, ErrNoToken
	}
	return f.token, f.provider, nil
}
func (f *fakeTokens) Delete(context.Context, string) error {
	f.mu.Lock()
	f.deleted++
	f.mu.Unlock()
	return nil
}
func (f *fakeTokens) deletes() int { f.mu.Lock(); defer f.mu.Unlock(); return f.deleted }

// scriptedDriver returns queued errors (nil = success), then its default.
type scriptedDriver struct {
	provider Provider
	mu       sync.Mutex
	queue    []error
	def      error
	sends    int
}

func (d *scriptedDriver) Provider() Provider { return d.provider }
func (d *scriptedDriver) Send(context.Context, string, Payload) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.sends++
	if len(d.queue) > 0 {
		e := d.queue[0]
		d.queue = d.queue[1:]
		return e
	}
	return d.def
}
func (d *scriptedDriver) sendCount() int { d.mu.Lock(); defer d.mu.Unlock(); return d.sends }

func newSvc(tokens TokenStore, driver PushDriver) *Service {
	return NewService(tokens, []PushDriver{driver}, 5, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

var dispatch = Dispatch{RecipientDeviceID: "d1", Payload: Payload{Kind: KindMessage, CollapseKey: "c1"}}

func TestHandle_SuccessAcks(t *testing.T) {
	drv := &scriptedDriver{provider: ProviderFCM}
	tok := &fakeTokens{token: "t", provider: ProviderFCM}
	if got := newSvc(tok, drv).Handle(context.Background(), dispatch, 1); got != Ack {
		t.Fatalf("success → %v, want Ack", got)
	}
	if drv.sendCount() != 1 {
		t.Fatal("driver should have been called once")
	}
}

func TestHandle_NoTokenAcks(t *testing.T) {
	drv := &scriptedDriver{provider: ProviderFCM}
	tok := &fakeTokens{noToken: true}
	if got := newSvc(tok, drv).Handle(context.Background(), dispatch, 1); got != Ack {
		t.Fatalf("no token → %v, want Ack (nothing to deliver)", got)
	}
	if drv.sendCount() != 0 {
		t.Fatal("driver must not be called when there is no token")
	}
}

func TestHandle_InvalidTokenDeletesAndAcks(t *testing.T) {
	drv := &scriptedDriver{provider: ProviderFCM, def: ErrTokenInvalid}
	tok := &fakeTokens{token: "t", provider: ProviderFCM}
	if got := newSvc(tok, drv).Handle(context.Background(), dispatch, 1); got != Ack {
		t.Fatalf("invalid token → %v, want Ack", got)
	}
	if tok.deletes() != 1 {
		t.Fatal("invalid token should be deleted")
	}
}

func TestHandle_TransientNacksThenDLQ(t *testing.T) {
	drv := &scriptedDriver{provider: ProviderFCM, def: errors.New("provider 503")}
	tok := &fakeTokens{token: "t", provider: ProviderFCM}
	s := newSvc(tok, drv)
	// Deliveries below the ceiling → Nack (redeliver).
	if got := s.Handle(context.Background(), dispatch, 1); got != Nack {
		t.Fatalf("transient (delivery 1) → %v, want Nack", got)
	}
	// At the ceiling (5) → Dead (DLQ), never dropped silently.
	if got := s.Handle(context.Background(), dispatch, 5); got != Dead {
		t.Fatalf("transient (delivery 5) → %v, want Dead", got)
	}
}

// The headline guarantee: once the breaker is open, dispatches are NACKed
// (redelivered) and the driver is not even called — a provider outage delays
// pushes, never drops them.
func TestHandle_BreakerOpenNacksWithoutSending(t *testing.T) {
	drv := &scriptedDriver{provider: ProviderFCM, def: errors.New("provider down")}
	tok := &fakeTokens{token: "t", provider: ProviderFCM}
	s := newSvc(tok, drv)

	// Drive the breaker open: DefaultThreshold (5) consecutive failures.
	for i := 0; i < DefaultThreshold; i++ {
		if got := s.Handle(context.Background(), dispatch, 1); got != Nack {
			t.Fatalf("failure %d → %v, want Nack", i, got)
		}
	}
	sendsBefore := drv.sendCount()

	// Breaker is now open: further dispatches Nack without touching the driver.
	for i := 0; i < 10; i++ {
		if got := s.Handle(context.Background(), dispatch, 1); got != Nack {
			t.Fatalf("breaker-open dispatch → %v, want Nack (never dropped)", got)
		}
	}
	if drv.sendCount() != sendsBefore {
		t.Fatalf("driver called %d times while breaker open; must be 0 (was %d, now %d)",
			drv.sendCount()-sendsBefore, sendsBefore, drv.sendCount())
	}
}
