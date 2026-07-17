package gateway

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/proto"

	"github.com/whatsapp-v2/server/internal/auth"
	wsv1 "github.com/whatsapp-v2/server/internal/proto/gen/whatsapp/ws/v1"
)

type denyAuthz struct{}

func (denyAuthz) Authorize(context.Context, auth.Identity) (bool, error) { return false, nil }

func newTestServer(t *testing.T, authz Authorizer) (*httptest.Server, *auth.TokenIssuer, *Server) {
	t.Helper()
	return newTestServerWithDelivery(t, authz, nil)
}

func newTestServerWithDelivery(t *testing.T, authz Authorizer, delivery DeliverySource) (*httptest.Server, *auth.TokenIssuer, *Server) {
	t.Helper()
	issuer, err := auth.NewEphemeralIssuer(10 * time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	s := NewServer(Config{
		Verifier:   issuer,
		Authorizer: authz,
		Routes:     NewMemoryRouteStore(),
		Delivery:   delivery,
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
	c, resp, err := websocket.Dial(ctx, wsURL(hs), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	return c
}

// writeFrame marshals and sends one binary protobuf frame (the client side
// of the wire contract).
func writeFrame(t *testing.T, c *websocket.Conn, f *wsv1.Frame) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	data, err := proto.Marshal(f)
	if err != nil {
		t.Fatalf("marshal frame: %v", err)
	}
	if err := c.Write(ctx, websocket.MessageBinary, data); err != nil {
		t.Fatalf("write frame: %v", err)
	}
}

// readServerFrame reads and decodes one binary protobuf frame.
func readServerFrame(t *testing.T, c *websocket.Conn) *wsv1.Frame {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	typ, data, err := c.Read(ctx)
	if err != nil {
		t.Fatalf("expected frame, got read error: %v (close=%d)", err, websocket.CloseStatus(err))
	}
	if typ != websocket.MessageBinary {
		t.Fatalf("frame message type = %d, want binary", typ)
	}
	f := &wsv1.Frame{}
	if err := proto.Unmarshal(data, f); err != nil {
		t.Fatalf("not a protobuf frame: %v", err)
	}
	return f
}

func sendHello(t *testing.T, c *websocket.Conn, token string) {
	t.Helper()
	writeFrame(t, c, &wsv1.Frame{
		Body: &wsv1.Frame_Hello{Hello: &wsv1.Hello{AccessJwt: token}},
	})
}

func expectAck(t *testing.T, c *websocket.Conn) *wsv1.HelloAck {
	t.Helper()
	f := readServerFrame(t, c)
	ack := f.GetHelloAck()
	if ack == nil {
		t.Fatalf("not a hello_ack: %v", f)
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
	if ack.GetSessionId() != "sess1" {
		t.Fatalf("ack session = %q, want sess1", ack.GetSessionId())
	}
	if _, ok := s.Registry().Get("d1"); !ok {
		t.Fatal("connection not in registry after handshake")
	}
	_ = c.Close(websocket.StatusNormalClosure, "")
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
	// A text message is a protocol violation — frames are binary protobuf.
	hs, _, _ := newTestServer(t, AllowAll{})
	c := dial(t, hs)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = c.Write(ctx, websocket.MessageText, []byte(`{"type":"not-hello"}`))
	expectClose(t, c, CloseBadHandshake)
}

func TestHandshake_WrongFirstFrame(t *testing.T) {
	// A well-formed frame that is not a hello must not pass the handshake.
	hs, _, _ := newTestServer(t, AllowAll{})
	c := dial(t, hs)
	writeFrame(t, c, &wsv1.Frame{
		Body: &wsv1.Frame_Ping{Ping: &wsv1.Ping{TsMs: 1}},
	})
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
	_ = c2.Close(websocket.StatusNormalClosure, "")
}
