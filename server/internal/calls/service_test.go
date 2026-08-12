package calls

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/whatsapp-v2/server/internal/auth"
	"github.com/whatsapp-v2/server/internal/calls/domain"
	"github.com/whatsapp-v2/server/internal/platform/httpx"
)

// ── fakes ────────────────────────────────────────────────────────────────

type fakeMinter struct{ minted int }

func (m *fakeMinter) Mint(g JoinGrant, _ time.Duration, _ time.Time) (string, error) {
	m.minted++
	return "token:" + g.Identity + "@" + g.Room, nil
}

type fakeRing struct {
	byRing map[string]RingRecord
	byRoom map[string]string
}

func newFakeRing() *fakeRing {
	return &fakeRing{byRing: map[string]RingRecord{}, byRoom: map[string]string{}}
}
func (r *fakeRing) Save(_ context.Context, rec RingRecord, _ time.Duration) error {
	r.byRing[rec.RingID] = rec
	r.byRoom[rec.RoomID] = rec.RingID
	return nil
}
func (r *fakeRing) Get(_ context.Context, ringID string) (RingRecord, error) {
	rec, ok := r.byRing[ringID]
	if !ok {
		return RingRecord{}, ErrNotFound
	}
	return rec, nil
}
func (r *fakeRing) GetByRoom(_ context.Context, roomID string) (RingRecord, error) {
	ringID, ok := r.byRoom[roomID]
	if !ok {
		return RingRecord{}, ErrNotFound
	}
	return r.Get(context.Background(), ringID)
}
func (r *fakeRing) ExpiredRinging(_ context.Context, now time.Time, _ int) ([]RingRecord, error) {
	var out []RingRecord
	for _, rec := range r.byRing {
		if rec.State == domain.StateRinging && !rec.Deadline.After(now) {
			out = append(out, rec)
		}
	}
	return out, nil
}

type fakeHistory struct {
	recs        map[string]CallRecord
	purgeCutoff time.Time
}

func newFakeHistory() *fakeHistory { return &fakeHistory{recs: map[string]CallRecord{}} }
func (h *fakeHistory) Upsert(_ context.Context, rec CallRecord) error {
	h.recs[rec.ID] = rec
	return nil
}
func (h *fakeHistory) List(_ context.Context, _, _ string, _ int) ([]CallRecord, string, error) {
	return nil, "", nil
}
func (h *fakeHistory) PurgeOlderThan(_ context.Context, cutoff time.Time) (int, error) {
	h.purgeCutoff = cutoff
	n := 0
	for id, rec := range h.recs {
		if rec.StartedAt != nil && rec.StartedAt.Before(cutoff) {
			delete(h.recs, id)
			n++
		}
	}
	return n, nil
}

type ringSignal struct {
	devices []string
	state   domain.RingState
}
type fakeSignaler struct {
	offers []CallOfferSignal
	rings  []ringSignal
	ends   int
}

func (s *fakeSignaler) Offer(_ context.Context, devs []string, o CallOfferSignal) error {
	s.offers = append(s.offers, o)
	_ = devs
	return nil
}
func (s *fakeSignaler) Ring(_ context.Context, devs []string, _ string, state domain.RingState, _ string) error {
	s.rings = append(s.rings, ringSignal{devices: devs, state: state})
	return nil
}
func (s *fakeSignaler) End(_ context.Context, _ []string, _, _, _ string) error {
	s.ends++
	return nil
}

type fakePusher struct{ voip, missed int }

func (p *fakePusher) VoIP(_ context.Context, _ []string, _ CallInvite) error { p.voip++; return nil }
func (p *fakePusher) Missed(_ context.Context, _ []string, _ string) error   { p.missed++; return nil }

type fakeDevices struct{ byUser map[string][]string }

func (d *fakeDevices) DevicesOf(_ context.Context, userIDs []string) (map[string][]string, error) {
	out := map[string][]string{}
	for _, u := range userIDs {
		if ds, ok := d.byUser[u]; ok {
			out[u] = ds
		}
	}
	return out, nil
}

type nopLogger struct{}

func (nopLogger) Warn(string, ...any) {}

type harness struct {
	svc    *Service
	minter *fakeMinter
	ring   *fakeRing
	hist   *fakeHistory
	sig    *fakeSignaler
	push   *fakePusher
	now    time.Time
	ids    []string
}

func newHarness() *harness {
	h := &harness{
		minter: &fakeMinter{}, ring: newFakeRing(), hist: newFakeHistory(),
		sig: &fakeSignaler{}, push: &fakePusher{}, now: time.Unix(1_800_000_000, 0),
	}
	devs := &fakeDevices{byUser: map[string][]string{
		"caller": {"cd1"},
		"bob":    {"bob-d1", "bob-d2"},
		"carol":  {"carol-d1"},
	}}
	h.svc = NewService(h.minter, h.ring, h.hist, h.sig, h.push, devs, nopLogger{})
	h.svc.now = func() time.Time { return h.now }
	var seq int
	h.svc.newID = func() string {
		seq++
		id := "id" + string(rune('0'+seq))
		h.ids = append(h.ids, id)
		return id
	}
	return h
}

func who(u, d string) auth.Identity { return auth.Identity{UserID: u, DeviceID: d, SessionID: "s"} }

func code(t *testing.T, err error) string {
	t.Helper()
	var ae *httpx.APIError
	if !errors.As(err, &ae) {
		t.Fatalf("want *httpx.APIError, got %T: %v", err, err)
	}
	return ae.Code
}

// ── tests ────────────────────────────────────────────────────────────────

func TestCreate_RingsCalleesAndMintsCallerToken(t *testing.T) {
	h := newHarness()
	res, err := h.svc.Create(context.Background(), who("caller", "cd1"), []string{"bob", "carol"}, domain.KindVideo)
	if err != nil {
		t.Fatal(err)
	}
	if res.JoinToken == "" || res.RoomID == "" || res.RingID == "" {
		t.Fatalf("incomplete result: %+v", res)
	}
	rec := h.ring.byRing[res.RingID]
	if rec.State != domain.StateRinging || rec.CallerID != "caller" {
		t.Fatalf("ring not opened correctly: %+v", rec)
	}
	if len(rec.CalleeIDs) != 2 {
		t.Fatalf("callees = %v", rec.CalleeIDs)
	}
	if len(h.sig.offers) != 1 || h.push.voip != 1 {
		t.Fatalf("expected 1 offer + 1 voip push, got offers=%d voip=%d", len(h.sig.offers), h.push.voip)
	}
}

func TestCreate_Validation(t *testing.T) {
	h := newHarness()
	if _, err := h.svc.Create(context.Background(), who("caller", "cd1"), nil, domain.KindVoice); code(t, err) != "VALIDATION_CALLEES" {
		t.Fatal("no callees should be VALIDATION_CALLEES")
	}
	if _, err := h.svc.Create(context.Background(), who("caller", "cd1"), []string{"bob"}, domain.CallKind(9)); code(t, err) != "VALIDATION_KIND" {
		t.Fatal("bad kind should be VALIDATION_KIND")
	}
	many := make([]string, domain.MaxCallees+1)
	for i := range many {
		many[i] = "u" + string(rune('a'+i%26)) + string(rune('0'+i/26))
	}
	if _, err := h.svc.Create(context.Background(), who("caller", "cd1"), many, domain.KindVoice); code(t, err) != "VALIDATION_CALLEES" {
		t.Fatal("too many callees should be VALIDATION_CALLEES")
	}
}

func TestAnswer_TransitionsAndSignals(t *testing.T) {
	h := newHarness()
	res, _ := h.svc.Create(context.Background(), who("caller", "cd1"), []string{"bob"}, domain.KindVoice)

	token, err := h.svc.Answer(context.Background(), who("bob", "bob-d1"), res.RingID)
	if err != nil {
		t.Fatal(err)
	}
	if token == "" {
		t.Fatal("answerer should get a join token")
	}
	rec := h.ring.byRing[res.RingID]
	if rec.State != domain.StateAnswered || rec.AnsweredBy != "bob-d1" {
		t.Fatalf("ring not answered: %+v", rec)
	}
	// Caller notified ANSWERED; bob's sibling device gets ANSWERED_ELSEWHERE.
	var sawAnswered, sawElsewhere bool
	for _, r := range h.sig.rings {
		if r.state == domain.StateAnswered {
			sawAnswered = true
		}
		if r.state == domain.StateAnsweredElsewhere {
			sawElsewhere = true
			if len(r.devices) != 1 || r.devices[0] != "bob-d2" {
				t.Fatalf("answered-elsewhere devices = %v, want [bob-d2]", r.devices)
			}
		}
	}
	if !sawAnswered || !sawElsewhere {
		t.Fatalf("missing ring signals (answered=%v elsewhere=%v)", sawAnswered, sawElsewhere)
	}
}

func TestAnswer_RejectsNonCalleeAndClosedRing(t *testing.T) {
	h := newHarness()
	res, _ := h.svc.Create(context.Background(), who("caller", "cd1"), []string{"bob"}, domain.KindVoice)

	if _, err := h.svc.Answer(context.Background(), who("mallory", "m1"), res.RingID); code(t, err) != "STATE_FORBIDDEN" {
		t.Fatal("non-callee should be STATE_FORBIDDEN")
	}
	// Decline closes the ring; a later answer is a conflict.
	if err := h.svc.Decline(context.Background(), who("bob", "bob-d1"), res.RingID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.Answer(context.Background(), who("bob", "bob-d1"), res.RingID); code(t, err) != "STATE_RING_CLOSED" {
		t.Fatal("answering a declined ring should be STATE_RING_CLOSED")
	}
}

func TestAnswer_SameDeviceReissuesToken(t *testing.T) {
	h := newHarness()
	res, _ := h.svc.Create(context.Background(), who("caller", "cd1"), []string{"bob"}, domain.KindVoice)
	if _, err := h.svc.Answer(context.Background(), who("bob", "bob-d1"), res.RingID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.Answer(context.Background(), who("bob", "bob-d1"), res.RingID); err != nil {
		t.Fatalf("re-answer from the same device should re-issue, got %v", err)
	}
}

func TestDecline_RecordsHistory(t *testing.T) {
	h := newHarness()
	res, _ := h.svc.Create(context.Background(), who("caller", "cd1"), []string{"bob"}, domain.KindVoice)
	if err := h.svc.Decline(context.Background(), who("bob", "bob-d1"), res.RingID); err != nil {
		t.Fatal(err)
	}
	rec, ok := h.hist.recs[res.RingID]
	if !ok || rec.Outcome != OutcomeDeclined {
		t.Fatalf("history = %+v (ok=%v), want declined", rec, ok)
	}
	// Idempotent: a second decline is a no-op, not an error.
	if err := h.svc.Decline(context.Background(), who("bob", "bob-d1"), res.RingID); err != nil {
		t.Fatalf("second decline should be idempotent, got %v", err)
	}
}

func TestSweepMissed_TransitionsAndPushes(t *testing.T) {
	h := newHarness()
	res, _ := h.svc.Create(context.Background(), who("caller", "cd1"), []string{"bob"}, domain.KindVoice)

	// Before the deadline → nothing swept.
	if n, _ := h.svc.SweepMissed(context.Background(), 10); n != 0 {
		t.Fatalf("swept %d before deadline, want 0", n)
	}
	// Past the deadline → missed.
	h.now = h.now.Add(domain.MissedAfter + time.Second)
	n, err := h.svc.SweepMissed(context.Background(), 10)
	if err != nil || n != 1 {
		t.Fatalf("sweep = (%d,%v), want (1,nil)", n, err)
	}
	if h.ring.byRing[res.RingID].State != domain.StateMissed {
		t.Fatal("ring should be missed")
	}
	if h.push.missed != 1 {
		t.Fatalf("missed pushes = %d, want 1", h.push.missed)
	}
	if h.hist.recs[res.RingID].Outcome != OutcomeMissed {
		t.Fatal("history should record missed")
	}
	// Idempotent: sweeping again does not re-miss.
	if n, _ := h.svc.SweepMissed(context.Background(), 10); n != 0 {
		t.Fatalf("re-sweep = %d, want 0", n)
	}
}

func TestRejoin_ParticipantOnlyWhileLive(t *testing.T) {
	h := newHarness()
	res, _ := h.svc.Create(context.Background(), who("caller", "cd1"), []string{"bob"}, domain.KindVoice)
	if _, err := h.svc.Answer(context.Background(), who("bob", "bob-d1"), res.RingID); err != nil {
		t.Fatal(err)
	}

	if _, err := h.svc.Rejoin(context.Background(), who("bob", "bob-d1"), res.RoomID); err != nil {
		t.Fatalf("participant rejoin: %v", err)
	}
	if _, err := h.svc.Rejoin(context.Background(), who("mallory", "m1"), res.RoomID); code(t, err) != "STATE_FORBIDDEN" {
		t.Fatal("non-participant rejoin should be STATE_FORBIDDEN")
	}
	if _, err := h.svc.Rejoin(context.Background(), who("bob", "bob-d1"), "call-nope"); code(t, err) != "CALL_NOT_FOUND" {
		t.Fatal("unknown room should be CALL_NOT_FOUND")
	}
}

func TestHandleWebhook_RoomFinishedForceEndsZombie(t *testing.T) {
	h := newHarness()
	res, _ := h.svc.Create(context.Background(), who("caller", "cd1"), []string{"bob"}, domain.KindVoice)
	_, _ = h.svc.Answer(context.Background(), who("bob", "bob-d1"), res.RingID)

	if err := h.svc.HandleWebhook(context.Background(), WebhookEvent{Event: "room_finished", Room: WebhookRoom{Name: res.RoomID}}); err != nil {
		t.Fatal(err)
	}
	if h.ring.byRing[res.RingID].State != domain.StateEnded {
		t.Fatal("room_finished should end the ring")
	}
	if h.hist.recs[res.RingID].Outcome != OutcomeCompleted {
		t.Fatalf("answered call that finished should be completed, got %+v", h.hist.recs[res.RingID])
	}
	// Idempotent redelivery.
	if err := h.svc.HandleWebhook(context.Background(), WebhookEvent{Event: "room_finished", Room: WebhookRoom{Name: res.RoomID}}); err != nil {
		t.Fatalf("redelivered webhook should be idempotent, got %v", err)
	}
}

func TestPurgeHistory_DeletesBeyondRetention(t *testing.T) {
	h := newHarness()
	old := h.now.Add(-100 * 24 * time.Hour) // beyond the 90-day window
	recent := h.now.Add(-1 * time.Hour)
	h.hist.recs["old"] = CallRecord{ID: "old", StartedAt: &old}
	h.hist.recs["recent"] = CallRecord{ID: "recent", StartedAt: &recent}

	n, err := h.svc.PurgeHistory(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("purged %d, want 1", n)
	}
	// Cutoff is exactly now − retention.
	if want := h.now.Add(-domain.HistoryRetention); !h.hist.purgeCutoff.Equal(want) {
		t.Errorf("cutoff = %v, want %v (now − 90d)", h.hist.purgeCutoff, want)
	}
	if _, ok := h.hist.recs["old"]; ok {
		t.Error("record beyond retention should be purged")
	}
	if _, ok := h.hist.recs["recent"]; !ok {
		t.Error("recent record must be retained")
	}
}
