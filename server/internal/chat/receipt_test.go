package chat

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
)

type fakeReceiptStore struct {
	targets     []string
	readEnabled bool
}

func (f *fakeReceiptStore) ReceiptTargets(_ context.Context, _, _, _ string) ([]string, error) {
	return f.targets, nil
}
func (f *fakeReceiptStore) ReadReceiptsEnabled(_ context.Context, _ string) (bool, error) {
	return f.readEnabled, nil
}

type recRelay struct {
	mu   sync.Mutex
	sent []ReceiptOut
}

func (r *recRelay) RelayReceipt(_ context.Context, _ string, out ReceiptOut) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sent = append(r.sent, out)
	return nil
}

func newReceiptSvc(readEnabled bool, targets []string) (*Service, *recRelay) {
	s := NewService(newMemStore(), nil, newMemDeduper(), nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	relay := &recRelay{}
	s.SetReceipts(&fakeReceiptStore{targets: targets, readEnabled: readEnabled}, relay)
	return s, relay
}

// Delivered receipts always relay, regardless of the read-receipt privacy
// setting.
func TestSubmitReceipt_DeliveredAlwaysRelays(t *testing.T) {
	s, relay := newReceiptSvc(false /* read receipts OFF */, []string{"devA1", "devA2"})
	err := s.SubmitReceipt(context.Background(), "u1", "devU1",
		ReceiptIn{ConversationID: "c1", Kind: ReceiptDelivered, UpToSeq: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(relay.sent) != 2 {
		t.Fatalf("delivered receipt relayed to %d devices, want 2", len(relay.sent))
	}
	if relay.sent[0].FromUserID != "u1" || relay.sent[0].UpToSeq != 5 {
		t.Fatalf("relayed receipt wrong: %+v", relay.sent[0])
	}
}

// The privacy gate: a user with read receipts disabled sends no READ receipts.
func TestSubmitReceipt_ReadPrivacyGate(t *testing.T) {
	s, relay := newReceiptSvc(false /* read receipts OFF */, []string{"devA1"})
	err := s.SubmitReceipt(context.Background(), "u1", "devU1",
		ReceiptIn{ConversationID: "c1", Kind: ReceiptRead, UpToSeq: 9})
	if err != nil {
		t.Fatalf("privacy-suppressed receipt must not error: %v", err)
	}
	if len(relay.sent) != 0 {
		t.Fatal("read receipt relayed despite privacy setting OFF")
	}
}

// With read receipts enabled, READ receipts relay normally.
func TestSubmitReceipt_ReadRelaysWhenEnabled(t *testing.T) {
	s, relay := newReceiptSvc(true /* read receipts ON */, []string{"devA1"})
	if err := s.SubmitReceipt(context.Background(), "u1", "devU1",
		ReceiptIn{ConversationID: "c1", Kind: ReceiptRead, UpToSeq: 9}); err != nil {
		t.Fatal(err)
	}
	if len(relay.sent) != 1 || relay.sent[0].Kind != ReceiptRead {
		t.Fatalf("read receipt not relayed when enabled: %+v", relay.sent)
	}
}

func TestSubmitReceipt_Validation(t *testing.T) {
	s, _ := newReceiptSvc(true, nil)
	ctx := context.Background()
	if err := s.SubmitReceipt(ctx, "u1", "d1", ReceiptIn{Kind: ReceiptRead, UpToSeq: 1}); code(t, err) != "VALIDATION_RECEIPT" {
		t.Fatal("missing conversation accepted")
	}
	if err := s.SubmitReceipt(ctx, "u1", "d1", ReceiptIn{ConversationID: "c1", Kind: ReceiptRead, UpToSeq: 0}); code(t, err) != "VALIDATION_RECEIPT" {
		t.Fatal("non-positive seq accepted")
	}
	if err := s.SubmitReceipt(ctx, "u1", "d1", ReceiptIn{ConversationID: "c1", Kind: 9, UpToSeq: 1}); code(t, err) != "VALIDATION_RECEIPT" {
		t.Fatal("bad kind accepted")
	}
}

// A service without receipts wired treats submissions as no-ops (accept-only
// deployments), never errors.
func TestSubmitReceipt_NotWired(t *testing.T) {
	s := NewService(newMemStore(), nil, newMemDeduper(), nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := s.SubmitReceipt(context.Background(), "u1", "d1",
		ReceiptIn{ConversationID: "c1", Kind: ReceiptDelivered, UpToSeq: 1}); err != nil {
		t.Fatalf("unwired receipts must no-op, got %v", err)
	}
}
