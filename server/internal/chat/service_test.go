package chat

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/whatsapp-v2/server/internal/platform/httpx"
	"github.com/whatsapp-v2/server/internal/platform/id"
)

// ── fakes ────────────────────────────────────────────────────────────────

type memStore struct {
	mu        sync.Mutex
	seqs      map[string]int64
	accepts   int
	notMember bool
}

func newMemStore() *memStore { return &memStore{seqs: map[string]int64{}} }

func (m *memStore) Accept(_ context.Context, p AcceptParams) (StoreResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.notMember {
		return StoreResult{}, ErrNotMember
	}
	m.accepts++
	m.seqs[p.ConversationID]++
	return StoreResult{Seq: m.seqs[p.ConversationID], RecipientDeviceIDs: []string{"devB1", "devB2"}}, nil
}

// recPublisher records deliveries; failPub always errors.
type recPublisher struct {
	mu    sync.Mutex
	items map[string][]InboxItem // by device id
}

func newRecPublisher() *recPublisher { return &recPublisher{items: map[string][]InboxItem{}} }

func (p *recPublisher) PublishDelivery(_ context.Context, deviceID string, item InboxItem) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.items[deviceID] = append(p.items[deviceID], item)
	return nil
}

type failPub struct{}

func (failPub) PublishDelivery(context.Context, string, InboxItem) error {
	return errors.New("nats down")
}

type memDeduper struct {
	mu sync.Mutex
	m  map[string]string // "P" or the committed seq
}

func newMemDeduper() *memDeduper { return &memDeduper{m: map[string]string{}} }

func (d *memDeduper) Claim(_ context.Context, u string) (bool, int64, bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	v, ok := d.m[u]
	if !ok {
		d.m[u] = "P"
		return true, 0, false, nil
	}
	if v == "P" {
		return false, 0, true, nil
	}
	seq, _ := strconv.ParseInt(v, 10, 64)
	return false, seq, false, nil
}
func (d *memDeduper) Commit(_ context.Context, u string, seq int64) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.m[u] = strconv.FormatInt(seq, 10)
	return nil
}
func (d *memDeduper) Release(_ context.Context, u string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.m, u)
	return nil
}

type errDeduper struct{}

func (errDeduper) Claim(context.Context, string) (bool, int64, bool, error) {
	return false, 0, false, errors.New("valkey down")
}
func (errDeduper) Commit(context.Context, string, int64) error { return nil }
func (errDeduper) Release(context.Context, string) error       { return nil }

func newSvc(store Store, dedupe Deduper) *Service {
	return NewService(store, nil, dedupe, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func req(conv, msgUUID string) AcceptRequest {
	return AcceptRequest{
		SenderUserID: "u1", SenderDeviceID: "dev1", ConversationID: conv,
		MsgUUID: msgUUID, Kind: KindText, Ciphertext: []byte("sealed"),
	}
}

func code(t *testing.T, err error) string {
	t.Helper()
	var ae *httpx.APIError
	if !errors.As(err, &ae) {
		t.Fatalf("want *httpx.APIError, got %T: %v", err, err)
	}
	return ae.Code
}

// ── tests ─────────────────────────────────────────────────────────────────

func TestAccept_AssignsSeq(t *testing.T) {
	store, dedupe := newMemStore(), newMemDeduper()
	s := newSvc(store, dedupe)
	ctx := context.Background()

	r1, err := s.Accept(ctx, req("c1", id.New()))
	if err != nil {
		t.Fatal(err)
	}
	r2, err := s.Accept(ctx, req("c1", id.New()))
	if err != nil {
		t.Fatal(err)
	}
	if r1.Seq != 1 || r2.Seq != 2 {
		t.Fatalf("seqs = %d,%d, want 1,2", r1.Seq, r2.Seq)
	}
	if r1.Deduped || r2.Deduped {
		t.Fatal("fresh sends must not be deduped")
	}
}

// The headline guarantee: a duplicate MsgSend returns an identical ack and
// does not re-run the accept transaction.
func TestAccept_DuplicateReturnsIdenticalAck(t *testing.T) {
	store, dedupe := newMemStore(), newMemDeduper()
	s := newSvc(store, dedupe)
	ctx := context.Background()

	u := id.New()
	first, err := s.Accept(ctx, req("c1", u))
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Accept(ctx, req("c1", u))
	if err != nil {
		t.Fatal(err)
	}
	if second.Seq != first.Seq {
		t.Fatalf("duplicate seq = %d, want identical %d", second.Seq, first.Seq)
	}
	if !second.Deduped {
		t.Fatal("duplicate must be flagged deduped")
	}
	if store.accepts != 1 {
		t.Fatalf("store.Accept ran %d times, want 1 (duplicate must not re-accept)", store.accepts)
	}
}

func TestAccept_FailsClosedOnDedupeError(t *testing.T) {
	store := newMemStore()
	s := newSvc(store, errDeduper{})
	if _, err := s.Accept(context.Background(), req("c1", id.New())); code(t, err) != "TRANSIENT_UNAVAILABLE" {
		t.Fatal("dedupe error must fail closed")
	}
	if store.accepts != 0 {
		t.Fatal("accept must not run when dedupe is unavailable")
	}
}

func TestAccept_PendingRetries(t *testing.T) {
	dedupe := newMemDeduper()
	s := newSvc(newMemStore(), dedupe)
	ctx := context.Background()
	u := id.New()
	// Simulate an in-flight accept: claim pending without committing.
	dedupe.m[u] = "P"
	if _, err := s.Accept(ctx, req("c1", u)); code(t, err) != "TRANSIENT_UNAVAILABLE" {
		t.Fatal("a pending duplicate must be told to retry")
	}
}

func TestAccept_NotMember(t *testing.T) {
	store := newMemStore()
	store.notMember = true
	dedupe := newMemDeduper()
	s := newSvc(store, dedupe)
	u := id.New()
	if _, err := s.Accept(context.Background(), req("c1", u)); code(t, err) != "STATE_NOT_MEMBER" {
		t.Fatal("non-member send must be rejected")
	}
	// The claim must be released so a legitimate retry could proceed.
	if _, ok := dedupe.m[u]; ok {
		t.Fatal("claim not released after failed accept")
	}
}

func TestAccept_Validation(t *testing.T) {
	s := newSvc(newMemStore(), newMemDeduper())
	ctx := context.Background()

	bad := req("c1", "not-a-uuid")
	if _, err := s.Accept(ctx, bad); code(t, err) != "VALIDATION_MSG_ID" {
		t.Fatal("bad msg_uuid accepted")
	}
	empty := req("c1", id.New())
	empty.Ciphertext = nil
	if _, err := s.Accept(ctx, empty); code(t, err) != "VALIDATION_EMPTY" {
		t.Fatal("empty ciphertext accepted")
	}
	noTarget := req("c1", id.New())
	noTarget.Kind = KindOverlayEdit
	if _, err := s.Accept(ctx, noTarget); code(t, err) != "VALIDATION_OVERLAY" {
		t.Fatal("overlay without target accepted")
	}
}

func TestAccept_PublishesToEveryRecipient(t *testing.T) {
	pub := newRecPublisher()
	s := NewService(newMemStore(), nil, newMemDeduper(), pub, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx := context.Background()

	u := id.New()
	res, err := s.Accept(ctx, req("c1", u))
	if err != nil {
		t.Fatal(err)
	}
	if res.RecipientCount != 2 {
		t.Fatalf("recipient count = %d, want 2", res.RecipientCount)
	}
	for _, dev := range []string{"devB1", "devB2"} {
		items := pub.items[dev]
		if len(items) != 1 {
			t.Fatalf("device %s got %d deliveries, want 1", dev, len(items))
		}
		if items[0].MsgUUID != u || items[0].Seq != res.Seq || string(items[0].Ciphertext) != "sealed" {
			t.Fatalf("delivery payload wrong: %+v", items[0])
		}
	}
	// A duplicate must NOT publish again.
	if _, err := s.Accept(ctx, req("c1", u)); err != nil {
		t.Fatal(err)
	}
	if len(pub.items["devB1"]) != 1 {
		t.Fatal("duplicate send re-published a delivery")
	}
}

// Publish failure is a latency event, never a loss or an error to the sender.
func TestAccept_PublishFailureDoesNotFailAccept(t *testing.T) {
	s := NewService(newMemStore(), nil, newMemDeduper(), failPub{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	res, err := s.Accept(context.Background(), req("c1", id.New()))
	if err != nil {
		t.Fatalf("accept failed on publish error: %v", err)
	}
	if res.Seq != 1 {
		t.Fatalf("seq = %d, want 1", res.Seq)
	}
}

func TestAccept_EditWindowClosed(t *testing.T) {
	s := newSvc(newMemStore(), newMemDeduper())
	target := id.NewUUID()
	// Advance the service clock past the edit window relative to the target.
	s.now = func() time.Time { return id.TimeOf(target).Add(16 * time.Minute) }

	r := req("c1", id.New())
	r.Kind = KindOverlayEdit
	r.OverlayTarget = target.String()
	if _, err := s.Accept(context.Background(), r); code(t, err) != "VALIDATION_EDIT_WINDOW_CLOSED" {
		t.Fatal("edit past 15m accepted")
	}
}
