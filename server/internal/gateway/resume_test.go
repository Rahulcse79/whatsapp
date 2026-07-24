package gateway

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/whatsapp-v2/server/internal/auth"
	commonv1 "github.com/whatsapp-v2/server/internal/proto/gen/whatsapp/common/v1"
	wsv1 "github.com/whatsapp-v2/server/internal/proto/gen/whatsapp/ws/v1"
)

// fakeChat is an in-process ChatClient: PullInbox streams a fixed inbox and
// records the cursors it was asked to resume from; AckDelivered records the
// watermarks the gateway forwarded.
type fakeChat struct {
	mu            sync.Mutex
	inbox         []*wsv1.InboxItem
	batch         int
	pulledCursors [][]*wsv1.ConversationCursor
	ackedCursors  [][]*wsv1.ConversationCursor
	receipts      []*wsv1.Receipt
}

func (f *fakeChat) AcceptMessage(_ context.Context, _, _ string, msg *wsv1.MsgSend) (*wsv1.MsgAck, *commonv1.Error, error) {
	return &wsv1.MsgAck{MsgUuid: msg.GetMsgUuid(), ConversationId: msg.GetConversationId(), Seq: 1}, nil, nil
}

func (f *fakeChat) PullInbox(_ context.Context, _ string, cursors []*wsv1.ConversationCursor, emit func([]*wsv1.InboxItem) error) error {
	f.mu.Lock()
	f.pulledCursors = append(f.pulledCursors, cursors)
	items, b := f.inbox, f.batch
	f.mu.Unlock()
	if b <= 0 {
		b = len(items)
	}
	for i := 0; i < len(items); i += b {
		end := i + b
		if end > len(items) {
			end = len(items)
		}
		if err := emit(items[i:end]); err != nil {
			return err
		}
	}
	return nil
}

func (f *fakeChat) AckDelivered(_ context.Context, _ string, upTo []*wsv1.ConversationCursor) error {
	f.mu.Lock()
	f.ackedCursors = append(f.ackedCursors, upTo)
	f.mu.Unlock()
	return nil
}

func (f *fakeChat) SubmitReceipt(_ context.Context, _, _ string, r *wsv1.Receipt) error {
	f.mu.Lock()
	f.receipts = append(f.receipts, r)
	f.mu.Unlock()
	return nil
}

func (f *fakeChat) submittedReceipts() []*wsv1.Receipt {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*wsv1.Receipt, len(f.receipts))
	copy(out, f.receipts)
	return out
}

func (f *fakeChat) lastPull() []*wsv1.ConversationCursor {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.pulledCursors) == 0 {
		return nil
	}
	return f.pulledCursors[len(f.pulledCursors)-1]
}

func newResumeServer(t *testing.T, chat ChatClient, resume ResumeStore) (*httptest.Server, *auth.TokenIssuer) {
	t.Helper()
	issuer, err := auth.NewEphemeralIssuer(10 * time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	s := NewServer(Config{
		Verifier:   issuer,
		Authorizer: AllowAll{},
		Routes:     NewMemoryRouteStore(),
		Delivery:   NewMemoryDeliverySource(),
		Chat:       chat,
		Resume:     resume,
		PodID:      "pod-test",
		Log:        slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/ws", s.Handle)
	hs := httptest.NewServer(mux)
	t.Cleanup(hs.Close)
	return hs, issuer
}

// readReplay collects inbox items across batches until the replay_complete
// marker, asserting every replay batch is flagged replay=true.
func readReplay(t *testing.T, c *websocket.Conn) []*wsv1.InboxItem {
	t.Helper()
	var got []*wsv1.InboxItem
	for i := 0; i < 100; i++ {
		f := readServerFrame(t, c)
		ib := f.GetInboxBatch()
		if ib == nil {
			continue
		}
		if !ib.GetReplay() {
			t.Fatalf("replay batch missing replay flag: %v", ib)
		}
		got = append(got, ib.GetItems()...)
		if ib.GetReplayComplete() {
			return got
		}
	}
	t.Fatal("never saw replay_complete")
	return nil
}

func TestResume_ReplayDeliversInboxInOrder(t *testing.T) {
	chat := &fakeChat{
		inbox: []*wsv1.InboxItem{item("c1", 1), item("c1", 2), item("c2", 1)},
		batch: 2,
	}
	hs, issuer := newResumeServer(t, chat, NewMemoryResumeStore())
	tok, _ := issuer.Issue("u1", "d1", "sess1")

	c := dial(t, hs)
	sendHello(t, c, tok)
	ack := expectAck(t, c)
	if !ack.GetReplayPending() {
		t.Fatal("hello_ack should signal replay pending when a chat backend exists")
	}
	if ack.GetResumeToken() == "" {
		t.Fatal("hello_ack should carry a rotated resume token")
	}

	got := readReplay(t, c)
	if len(got) != 3 {
		t.Fatalf("replayed %d items, want 3", len(got))
	}
	want := []struct {
		conv string
		seq  int64
	}{{"c1", 1}, {"c1", 2}, {"c2", 1}}
	for i, w := range want {
		if got[i].GetConversationId() != w.conv || got[i].GetSeq() != w.seq {
			t.Fatalf("item[%d] = %s/%d, want %s/%d", i,
				got[i].GetConversationId(), got[i].GetSeq(), w.conv, w.seq)
		}
	}
	_ = c.Close(websocket.StatusNormalClosure, "")
}

func TestResume_ValidTokenHonorsCursors(t *testing.T) {
	resume := NewMemoryResumeStore()
	chat := &fakeChat{} // empty inbox — we only care about the cursors it's pulled with
	hs, issuer := newResumeServer(t, chat, resume)
	tok, _ := issuer.Issue("u1", "d1", "sess1")

	// First connect: no resume token → fresh replay (empty cursors).
	c1 := dial(t, hs)
	sendHello(t, c1, tok)
	ack1 := expectAck(t, c1)
	readReplay(t, c1)
	if cur := chat.lastPull(); len(cur) != 0 {
		t.Fatalf("first connect should pull with no cursors, got %v", cur)
	}
	resumeToken := ack1.GetResumeToken()
	_ = c1.Close(websocket.StatusNormalClosure, "")

	// Reconnect WITH the valid resume token + cursors → cursors honored.
	c2 := dial(t, hs)
	writeFrame(t, c2, &wsv1.Frame{Body: &wsv1.Frame_Hello{Hello: &wsv1.Hello{
		AccessJwt:   tok,
		ResumeToken: resumeToken,
		LastCursors: []*wsv1.ConversationCursor{{ConversationId: "c1", LastSeq: 5}},
	}}})
	expectAck(t, c2)
	readReplay(t, c2)
	cur := chat.lastPull()
	if len(cur) != 1 || cur[0].GetConversationId() != "c1" || cur[0].GetLastSeq() != 5 {
		t.Fatalf("valid resume should honor cursors, got %v", cur)
	}
	_ = c2.Close(websocket.StatusNormalClosure, "")
}

func TestResume_InvalidTokenDegradesToFullReplay(t *testing.T) {
	chat := &fakeChat{}
	hs, issuer := newResumeServer(t, chat, NewMemoryResumeStore())
	tok, _ := issuer.Issue("u1", "d1", "sess1")

	c := dial(t, hs)
	// A forged/stale resume token with cursors: cursors must be IGNORED
	// (degrade to full replay) — never trust cursors on an unvalidated token.
	writeFrame(t, c, &wsv1.Frame{Body: &wsv1.Frame_Hello{Hello: &wsv1.Hello{
		AccessJwt:   tok,
		ResumeToken: "forged-token",
		LastCursors: []*wsv1.ConversationCursor{{ConversationId: "c1", LastSeq: 99}},
	}}})
	expectAck(t, c)
	readReplay(t, c)
	if cur := chat.lastPull(); len(cur) != 0 {
		t.Fatalf("invalid token must drop cursors (full replay), got %v", cur)
	}
	_ = c.Close(websocket.StatusNormalClosure, "")
}

func TestResume_ClientAckForwarded(t *testing.T) {
	chat := &fakeChat{}
	hs, issuer := newResumeServer(t, chat, NewMemoryResumeStore())
	tok, _ := issuer.Issue("u1", "d1", "sess1")

	c := dial(t, hs)
	sendHello(t, c, tok)
	expectAck(t, c)
	readReplay(t, c)

	writeFrame(t, c, &wsv1.Frame{Body: &wsv1.Frame_ClientAck{ClientAck: &wsv1.ClientAck{
		UpTo: []*wsv1.ConversationCursor{{ConversationId: "c1", LastSeq: 3}},
	}}})

	// The forward is async; poll briefly for it.
	deadline := time.Now().Add(3 * time.Second)
	for {
		chat.mu.Lock()
		n := len(chat.ackedCursors)
		chat.mu.Unlock()
		if n > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("client ack was never forwarded to core-api")
		}
		time.Sleep(10 * time.Millisecond)
	}
	acked := chat.ackedCursors[0]
	if len(acked) != 1 || acked[0].GetConversationId() != "c1" || acked[0].GetLastSeq() != 3 {
		t.Fatalf("forwarded ack cursors = %v, want c1/3", acked)
	}
	_ = c.Close(websocket.StatusNormalClosure, "")
}
