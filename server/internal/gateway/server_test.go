package gateway

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/whatsapp-v2/server/internal/auth"
)

type denyAuthz struct{}

func (denyAuthz) Authorize(context.Context, auth.Identity) (bool, error) { return false, nil }

func newTestServer(t *testing.T, authz Authorizer) (*httptest.Server, *auth.TokenIssuer, *Server) {
	t.Helper()
	issuer, err := auth.NewEphemeralIssuer(10 * time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	s := NewServer(Config{
		Verifier:   issuer,
		Authorizer: authz,
		Routes:     NewMemoryRouteStore(),
		PodID:      "pod-test",
		Log:        slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/ws", s.Handle)
	hs := httptest.NewServer(mux)
	t.Cleanup(hs.Close)
	return hs, issuer, s
}

func wsURL(hs *httptest.Server) string {
	return "ws" + strings.TrimPrefix(hs.URL, "http") + "/v1/ws"
}

func dial(t *testing.T, hs *httptest.Server) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, wsURL(hs), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	return c
}

func sendHello(t *testing.T, c *websocket.Conn, token string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	data, _ := json.Marshal(helloFrame{Type: frameHello, AccessJWT: token})
	if err := c.Write(ctx, websocket.MessageText, data); err != nil {
		t.Fatalf("write hello: %v", err)
	}
}

func expectAck(t *testing.T, c *websocket.Conn) helloAckFrame {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, data, err := c.Read(ctx)
	if err != nil {
		t.Fatalf("expected hello_ack, got read error: %v (close=%d)", err, websocket.CloseStatus(err))
	}
	var ack helloAckFrame
	if err := json.Unmarshal(data, &ack); err != nil || ack.Type != frameHelloAck {
		t.Fatalf("not a hello_ack: %s", data)
	}
	return ack
}

// expectClose reads (skipping any pre-close message frames) until the
// connection closes, and asserts the close code.
func expectClose(t *testing.T, c *websocket.Conn, want CloseCode) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	for {
		_, _, err := c.Read(ctx)
		if err != nil {
			if got := websocket.CloseStatus(err); got != want {
				t.Fatalf("close code = %d, want %d (err %v)", got, want, err)
			}
			return
		}
	}
}

func TestHandshake_Success(t *testing.T) {
	hs, issuer, s := newTestServer(t, AllowAll{})
	tok, _ := issuer.Issue("u1", "d1", "sess1")

	c := dial(t, hs)
	sendHello(t, c, tok)
	ack := expectAck(t, c)
	if ack.SessionID != "sess1" {
		t.Fatalf("ack session = %q, want sess1", ack.SessionID)
	}
	if _, ok := s.Registry().Get("d1"); !ok {
		t.Fatal("connection not in registry after handshake")
	}
	c.Close(websocket.StatusNormalClosure, "")
}

func TestHandshake_BadToken(t *testing.T) {
	hs, _, _ := newTestServer(t, AllowAll{})
	c := dial(t, hs)
	sendHello(t, c, "not-a-jwt")
	expectClose(t, c, CloseAuthExpired)
}

func TestHandshake_Revoked(t *testing.T) {
	hs, issuer, _ := newTestServer(t, denyAuthz{})
	tok, _ := issuer.Issue("u1", "d1", "sess1")
	c := dial(t, hs)
	sendHello(t, c, tok)
	expectClose(t, c, CloseRevoked)
}

func TestHandshake_MalformedHello(t *testing.T) {
	hs, _, _ := newTestServer(t, AllowAll{})
	c := dial(t, hs)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = c.Write(ctx, websocket.MessageText, []byte(`{"type":"not-hello"}`))
	expectClose(t, c, CloseBadHandshake)
}

func TestDuplicateConnect_ClosesOldWith4409(t *testing.T) {
	hs, issuer, s := newTestServer(t, AllowAll{})
	tok, _ := issuer.Issue("u1", "dupdev", "sess1")

	c1 := dial(t, hs)
	sendHello(t, c1, tok)
	expectAck(t, c1)

	c2 := dial(t, hs)
	sendHello(t, c2, tok)
	expectAck(t, c2)

	// The first connection is displaced with 4409.
	expectClose(t, c1, CloseReplaced)

	// The registry holds exactly the second connection.
	if got, ok := s.Registry().Get("dupdev"); !ok || got.sessionID != "sess1" {
		t.Fatal("registry should hold the newer connection")
	}
	c2.Close(websocket.StatusNormalClosure, "")
}
