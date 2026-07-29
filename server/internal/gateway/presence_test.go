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
	"google.golang.org/protobuf/proto"

	"github.com/whatsapp-v2/server/internal/auth"
	"github.com/whatsapp-v2/server/internal/presence"
	wsv1 "github.com/whatsapp-v2/server/internal/proto/gen/whatsapp/ws/v1"
)

// fakePresence implements PresenceBackend: SubscribePresence/Typing capture the
// gateway's delivery callback so a test can inject fan-out; Publish* record.
type fakePresence struct {
	mu             sync.Mutex
	presDeliver    map[string]func([]byte)
	typDeliver     map[string]func([]byte)
	published      []presence.Update
	typed          []presence.TypingEvent
	canSee         bool
	snapshotOnline bool
}

func newFakePresence() *fakePresence {
	return &fakePresence{
		presDeliver: map[string]func([]byte){},
		typDeliver:  map[string]func([]byte){},
		canSee:      true,
	}
}

func (f *fakePresence) Connect(context.Context, string, string, int64) (bool, error) {
	return true, nil
}
func (f *fakePresence) Heartbeat(context.Context, string, string, int64) error { return nil }
func (f *fakePresence) Disconnect(context.Context, string, string, int64) (bool, error) {
	return true, nil
}
func (f *fakePresence) Snapshot(_ context.Context, userID string, _ int64) (presence.Update, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return presence.Update{UserID: userID, Online: f.snapshotOnline}, nil
}
func (f *fakePresence) PublishUpdate(_ context.Context, u presence.Update) error {
	f.mu.Lock()
	f.published = append(f.published, u)
	f.mu.Unlock()
	return nil
}
func (f *fakePresence) PublishTyping(_ context.Context, t presence.TypingEvent) error {
	f.mu.Lock()
	f.typed = append(f.typed, t)
	f.mu.Unlock()
	return nil
}
func (f *fakePresence) CanSeePresence(context.Context, string, string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.canSee, nil
}
func (f *fakePresence) SubscribePresence(userID string, deliver func([]byte)) (func(), error) {
	f.mu.Lock()
	f.presDeliver[userID] = deliver
	f.mu.Unlock()
	return func() { f.mu.Lock(); delete(f.presDeliver, userID); f.mu.Unlock() }, nil
}
func (f *fakePresence) SubscribeTyping(userID string, deliver func([]byte)) (func(), error) {
	f.mu.Lock()
	f.typDeliver[userID] = deliver
	f.mu.Unlock()
	return func() { f.mu.Lock(); delete(f.typDeliver, userID); f.mu.Unlock() }, nil
}

func (f *fakePresence) delivererFor(user string) func([]byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.presDeliver[user]
}
func (f *fakePresence) publishedUpdates() []presence.Update {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]presence.Update(nil), f.published...)
}
func (f *fakePresence) typingEvents() []presence.TypingEvent {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]presence.TypingEvent(nil), f.typed...)
}

func presenceUpdateBytes(t *testing.T, user string, online bool) []byte {
	t.Helper()
	b, err := proto.Marshal(&wsv1.PresenceUpdate{UserId: user, Online: online})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func newPresenceServer(t *testing.T, p PresenceBackend) (*httptest.Server, *auth.TokenIssuer) {
	t.Helper()
	issuer, err := auth.NewEphemeralIssuer(10 * time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	s := NewServer(Config{
		Verifier:      issuer,
		Authorizer:    AllowAll{},
		Routes:        NewMemoryRouteStore(),
		Presence:      p,
		PresenceGrace: 50 * time.Millisecond,
		PodID:         "pod-test",
		Log:           slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/ws", s.Handle)
	hs := httptest.NewServer(mux)
	t.Cleanup(hs.Close)
	return hs, issuer
}

// readPresence reads frames until a PresenceUpdate arrives (or times out).
func readPresence(t *testing.T, c *websocket.Conn) (*wsv1.PresenceUpdate, bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for {
		typ, data, err := c.Read(ctx)
		if err != nil {
			return nil, false
		}
		if typ != websocket.MessageBinary {
			continue
		}
		f := &wsv1.Frame{}
		if proto.Unmarshal(data, f) != nil {
			continue
		}
		if pu := f.GetPresenceUpdate(); pu != nil {
			return pu, true
		}
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met within timeout")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestPresence_ConnectPublishesOnline(t *testing.T) {
	fake := newFakePresence()
	hs, issuer := newPresenceServer(t, fake)
	tok, _ := issuer.Issue("u1", "d1", "sess1")

	c := dial(t, hs)
	sendHello(t, c, tok)
	expectAck(t, c)

	waitFor(t, func() bool { return len(fake.publishedUpdates()) > 0 })
	up := fake.publishedUpdates()[0]
	if up.UserID != "u1" || !up.Online {
		t.Fatalf("connect should publish online for u1, got %+v", up)
	}
	_ = c.Close(websocket.StatusNormalClosure, "")
}

func TestPresence_SubscribeReceivesSnapshotAndUpdates(t *testing.T) {
	fake := newFakePresence()
	hs, issuer := newPresenceServer(t, fake)
	tok, _ := issuer.Issue("u1", "d1", "sess1")

	c := dial(t, hs)
	sendHello(t, c, tok)
	expectAck(t, c)

	// Subscribe to user B.
	writeFrame(t, c, &wsv1.Frame{Body: &wsv1.Frame_PresenceSub{PresenceSub: &wsv1.PresenceSub{
		SubscribeUserIds: []string{"B"},
	}}})

	// Snapshot (online=false by default) arrives first.
	snap, ok := readPresence(t, c)
	if !ok || snap.GetUserId() != "B" || snap.GetOnline() {
		t.Fatalf("expected B snapshot online=false, got %+v ok=%v", snap, ok)
	}

	// Inject a live presence change for B via the captured deliverer.
	waitFor(t, func() bool { return fake.delivererFor("B") != nil })
	fake.delivererFor("B")(presenceUpdateBytes(t, "B", true))

	up, ok := readPresence(t, c)
	if !ok || up.GetUserId() != "B" || !up.GetOnline() {
		t.Fatalf("expected B online=true, got %+v ok=%v", up, ok)
	}
	_ = c.Close(websocket.StatusNormalClosure, "")
}

// The DONE guarantee: after unsubscribing, a stale in-flight update for that
// user is dropped by the filter — unsubscribed users never receive updates.
func TestPresence_UnsubscribeDropsUpdates(t *testing.T) {
	fake := newFakePresence()
	hs, issuer := newPresenceServer(t, fake)
	tok, _ := issuer.Issue("u1", "d1", "sess1")

	c := dial(t, hs)
	sendHello(t, c, tok)
	expectAck(t, c)

	writeFrame(t, c, &wsv1.Frame{Body: &wsv1.Frame_PresenceSub{PresenceSub: &wsv1.PresenceSub{
		SubscribeUserIds: []string{"B"},
	}}})
	readPresence(t, c) // consume the snapshot
	waitFor(t, func() bool { return fake.delivererFor("B") != nil })
	deliver := fake.delivererFor("B") // capture before unsubscribe

	// Unsubscribe B.
	writeFrame(t, c, &wsv1.Frame{Body: &wsv1.Frame_PresenceSub{PresenceSub: &wsv1.PresenceSub{
		UnsubscribeUserIds: []string{"B"},
	}}})
	// Let the server process the unsubscribe (subs.Has(B) becomes false).
	time.Sleep(100 * time.Millisecond)

	// A stale in-flight update must be dropped by the subs filter.
	deliver(presenceUpdateBytes(t, "B", true))
	if up, ok := readPresence(t, c); ok {
		t.Fatalf("received presence for unsubscribed user: %+v", up)
	}
	_ = c.Close(websocket.StatusNormalClosure, "")
}

func TestPresence_TypingPublished(t *testing.T) {
	fake := newFakePresence()
	hs, issuer := newPresenceServer(t, fake)
	tok, _ := issuer.Issue("u1", "d1", "sess1")

	c := dial(t, hs)
	sendHello(t, c, tok)
	expectAck(t, c)

	writeFrame(t, c, &wsv1.Frame{Body: &wsv1.Frame_Typing{Typing: &wsv1.Typing{
		ConversationId: "conv1", Recording: true,
	}}})

	waitFor(t, func() bool { return len(fake.typingEvents()) > 0 })
	ev := fake.typingEvents()[0]
	if ev.TyperUserID != "u1" || ev.ConversationID != "conv1" || !ev.Recording {
		t.Fatalf("typing not published correctly: %+v", ev)
	}
	_ = c.Close(websocket.StatusNormalClosure, "")
}
