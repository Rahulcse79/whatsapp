package gateway

import (
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/coder/websocket"

	wsv1 "github.com/whatsapp-v2/server/internal/proto/gen/whatsapp/ws/v1"
)

// ProtoClient is a reusable headless protobuf client for protocol-scenario
// tests (test-strategy.md §3). It speaks the real binary wire contract against
// an in-process gateway, so scenarios read like the sequence diagrams.
//
// SCOPE: this drives the gateway boundary using the in-memory delivery source
// (the proven harness in server_test.go / delivery_test.go). The full-stack
// variant — this same client against a real core-api + ws-gateway wired to
// PG/Valkey/NATS via testcontainers, adding offline→push→resume (P2) and the
// chaos-kill (P3) scenarios as a SEPARATE CI job — is authored in a
// Docker-capable environment (the remainder of T0.24; testcontainers + buf +
// local iteration are required and unavailable in the write-only CI-verifies
// workflow).
type ProtoClient struct {
	t    *testing.T
	conn *websocket.Conn
}

// Connect dials the in-process gateway.
func Connect(t *testing.T, hs *httptest.Server) *ProtoClient {
	t.Helper()
	return &ProtoClient{t: t, conn: dial(t, hs)}
}

// Hello performs the handshake and returns the HelloAck.
func (c *ProtoClient) Hello(token string) *wsv1.HelloAck {
	c.t.Helper()
	sendHello(c.t, c.conn, token)
	return expectAck(c.t, c.conn)
}

// Send transmits one client frame.
func (c *ProtoClient) Send(f *wsv1.Frame) {
	c.t.Helper()
	writeFrame(c.t, c.conn, f)
}

// Recv reads the next server frame.
func (c *ProtoClient) Recv() *wsv1.Frame {
	c.t.Helper()
	return readServerFrame(c.t, c.conn)
}

// ExpectInboxItem reads one live inbox item.
func (c *ProtoClient) ExpectInboxItem() *wsv1.InboxItem {
	c.t.Helper()
	return expectInboxItem(c.t, c.conn)
}

// ExpectClose asserts the connection closes with the given code.
func (c *ProtoClient) ExpectClose(code CloseCode) {
	c.t.Helper()
	expectClose(c.t, c.conn, code)
}

// Close ends the connection.
func (c *ProtoClient) Close() {
	_ = c.conn.Close(websocket.StatusNormalClosure, "")
}

// TestScenarioP1_LiveDeliveryInOrder is the gateway-boundary slice of P1/P8
// (test-strategy §3): after the handshake a burst of live deliveries arrives in
// per-conversation seq order, exactly once, through the reusable client. The
// cross-service send→core→NATS→recipient path and the offline/push/chaos
// scenarios run in the full-stack harness.
func TestScenarioP1_LiveDeliveryInOrder(t *testing.T) {
	source := NewMemoryDeliverySource()
	hs, issuer, _ := newTestServerWithDelivery(t, AllowAll{}, source)
	tok, err := issuer.Issue("u1", "p1dev", "sess1")
	if err != nil {
		t.Fatal(err)
	}

	cl := Connect(t, hs)
	defer cl.Close()
	cl.Hello(tok)

	const n = 10
	for i := 0; i < n; i++ {
		source.Publish("p1dev", deliveryPayload(t, "p1dev", fmt.Sprintf("m%02d", i), int64(i+1)))
	}
	for i := 0; i < n; i++ {
		item := cl.ExpectInboxItem()
		if got, want := item.GetMsgUuid(), fmt.Sprintf("m%02d", i); got != want {
			t.Fatalf("P1 ordering: got %s want %s", got, want)
		}
		if got, want := item.GetSeq(), int64(i+1); got != want {
			t.Fatalf("P1 seq: got %d want %d", got, want)
		}
	}
}
