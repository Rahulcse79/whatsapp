package ptt

import (
	"context"
	"testing"
	"time"

	"github.com/whatsapp-v2/server/internal/auth"
	"github.com/whatsapp-v2/server/internal/ptt/adapters"
	"github.com/whatsapp-v2/server/internal/ptt/domain"
)

// ── fakes ────────────────────────────────────────────────────────────────

type sfuCall struct {
	allow bool
	who   string
	fence int64
}
type fakeSFU struct{ calls []sfuCall }

func (s *fakeSFU) AllowPublish(_ context.Context, _, p string, fence int64) error {
	s.calls = append(s.calls, sfuCall{allow: true, who: p, fence: fence})
	return nil
}
func (s *fakeSFU) DenyPublish(_ context.Context, _, p string) error {
	s.calls = append(s.calls, sfuCall{allow: false, who: p})
	return nil
}
func (s *fakeSFU) lastAllow() *sfuCall {
	for i := len(s.calls) - 1; i >= 0; i-- {
		if s.calls[i].allow {
			return &s.calls[i]
		}
	}
	return nil
}
func (s *fakeSFU) denied(p string) bool {
	for _, c := range s.calls {
		if !c.allow && c.who == p {
			return true
		}
	}
	return false
}

type sig struct {
	grants  map[string]int64 // participant → fence
	queued  map[string]int   // participant → position
	revoked map[string]string
}
type fakeSig struct{ sig }

func newFakeSig() *fakeSig {
	return &fakeSig{sig{grants: map[string]int64{}, queued: map[string]int{}, revoked: map[string]string{}}}
}
func (f *fakeSig) Grant(_ context.Context, _, p string, fence, _ int64) error {
	f.grants[p] = fence
	return nil
}
func (f *fakeSig) QueuePos(_ context.Context, _, p string, pos int) error {
	f.queued[p] = pos
	return nil
}
func (f *fakeSig) Revoke(_ context.Context, _, p, reason string) error {
	f.revoked[p] = reason
	return nil
}

type nopLog struct{}

func (nopLog) Warn(string, ...any) {}

func ident(u, d string) auth.Identity { return auth.Identity{UserID: u, DeviceID: d, SessionID: "s"} }

type harness struct {
	svc   *Service
	store *adapters.MemoryFloorStore
	sfu   *fakeSFU
	sig   *fakeSig
	now   time.Time
}

func newHarness() *harness {
	h := &harness{store: adapters.NewMemoryFloorStore(), sfu: &fakeSFU{}, sig: newFakeSig(), now: time.Unix(1_800_000_000, 0)}
	h.store.Clock = func() time.Time { return h.now }
	h.svc = NewService(h.store, h.sfu, h.sig, nopLog{})
	return h
}

// ── tests ────────────────────────────────────────────────────────────────

func TestAcquire_GrantsFlipsSFUAndSignals(t *testing.T) {
	h := newHarness()
	if err := h.svc.Acquire(context.Background(), "room1", ident("u1", "d1")); err != nil {
		t.Fatal(err)
	}
	p := "u1:d1"
	if h.sig.grants[p] != 1 {
		t.Fatalf("want PttGrant fence 1 to %s, got %+v", p, h.sig.grants)
	}
	if a := h.sfu.lastAllow(); a == nil || a.who != p || a.fence != 1 {
		t.Fatalf("want SFU allow-publish %s fence 1, got %+v", p, a)
	}
}

func TestAcquire_SecondSpeakerQueues(t *testing.T) {
	h := newHarness()
	_ = h.svc.Acquire(context.Background(), "room1", ident("u1", "d1"))
	if err := h.svc.Acquire(context.Background(), "room1", ident("u2", "d2")); err != nil {
		t.Fatal(err)
	}
	if h.sig.queued["u2:d2"] != 1 {
		t.Fatalf("want u2 queued at position 1, got %+v", h.sig.queued)
	}
	if _, granted := h.sig.grants["u2:d2"]; granted {
		t.Fatal("queued speaker must not be granted the floor")
	}
}

func TestRelease_PromotesNextAndFlipsSFU(t *testing.T) {
	h := newHarness()
	_ = h.svc.Acquire(context.Background(), "room1", ident("u1", "d1"))
	_ = h.svc.Acquire(context.Background(), "room1", ident("u2", "d2")) // queued

	if err := h.svc.Release(context.Background(), "room1", ident("u1", "d1")); err != nil {
		t.Fatal(err)
	}
	if !h.sfu.denied("u1:d1") {
		t.Fatal("released speaker must be denied publish")
	}
	if h.sig.grants["u2:d2"] != 2 {
		t.Fatalf("want u2 promoted with fence 2, got %+v", h.sig.grants)
	}
	if a := h.sfu.lastAllow(); a == nil || a.who != "u2:d2" || a.fence != 2 {
		t.Fatalf("want SFU allow u2 fence 2, got %+v", a)
	}
}

func TestHeartbeat_LostRevokesAndMutes(t *testing.T) {
	h := newHarness()
	_ = h.svc.Acquire(context.Background(), "room1", ident("u1", "d1"))
	// A non-holder heartbeat (or a lapsed one) → lost.
	if err := h.svc.Heartbeat(context.Background(), "room1", ident("u2", "d2")); err != nil {
		t.Fatal(err)
	}
	if h.sig.revoked["u2:d2"] != "lost" || !h.sfu.denied("u2:d2") {
		t.Fatalf("non-holder heartbeat should revoke+mute; revoked=%+v", h.sig.revoked)
	}
}

func TestSweep_LapsedFloorPromotesHeadAndMutesZombie(t *testing.T) {
	h := newHarness()
	_ = h.svc.Acquire(context.Background(), "room1", ident("u1", "d1")) // u1 holds
	_ = h.svc.Acquire(context.Background(), "room1", ident("u2", "d2")) // u2 queued

	// u1 stops heartbeating; the lease lapses.
	h.now = h.now.Add(3 * time.Second)
	n, err := h.svc.SweepAll(context.Background())
	if err != nil || n != 1 {
		t.Fatalf("sweep = (%d,%v), want (1,nil)", n, err)
	}
	if !h.sfu.denied("u1:d1") {
		t.Fatal("lapsed holder u1 must be muted (fenced token superseded)")
	}
	if h.sig.grants["u2:d2"] != 2 {
		t.Fatalf("u2 should be promoted with fence 2, got %+v", h.sig.grants)
	}
	if h.sig.revoked["u1:d1"] != "lapsed" {
		t.Fatalf("u1 should be revoked as lapsed, got %+v", h.sig.revoked)
	}
}

var _ = domain.MaxSpeakMS // keep domain imported for the maxSpeakMS wire const
