package gateway

import (
	"context"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// CloseCode is a WebSocket close code. The 44xx range is application-defined
// (websocket-protocol.md §6).
type CloseCode = websocket.StatusCode

const (
	CloseBadHandshake CloseCode = 4400 // malformed or missing hello
	CloseAuthExpired  CloseCode = 4401 // auth token invalid/expired → refresh + reconnect
	CloseRevoked      CloseCode = 4403 // device revoked / account suspended
	CloseReplaced     CloseCode = 4409 // a newer connection for this device won the route
	CloseRateAbuse    CloseCode = 4429 // connection-level rate abuse
	CloseDrain        CloseCode = 1012 // server draining (deploy) → reconnect after hint
)

// Conn wraps a WebSocket connection with the identity established at
// handshake. Closing is idempotent.
type Conn struct {
	deviceID  string
	userID    string
	sessionID string
	ws        *websocket.Conn
	closeOnce sync.Once
}

// Close shuts the connection with a code+reason exactly once.
func (c *Conn) Close(code CloseCode, reason string) {
	c.closeOnce.Do(func() {
		// Best-effort: a peer that already vanished makes this error, which
		// is nothing we can act on.
		_ = c.ws.Close(code, reason)
	})
}

// write sends one message with a bounded deadline.
func (c *Conn) write(ctx context.Context, data []byte) error {
	wctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return c.ws.Write(wctx, websocket.MessageText, data)
}

// DeviceID exposes the connection's authenticated device (observability/tests).
func (c *Conn) DeviceID() string { return c.deviceID }
