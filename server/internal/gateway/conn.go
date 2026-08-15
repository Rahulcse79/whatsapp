package gateway

import (
	"context"
	"sync"
	"sync/atomic"
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
	// CloseSlowConsumer (1013 Try Again Later): the device fell behind the
	// bounded outbound queue. Nothing is lost — the backlog lives in the PG
	// inbox and resume replay delivers it on reconnect. Disconnecting is the
	// backpressure policy that keeps backlog out of pod memory
	// (ws-gateway-lld §2).
	CloseSlowConsumer CloseCode = 1013
)

// outboundQueueSize bounds per-connection buffered deliveries. At ~1 KB
// average frames this is well under the 1 MB high-water mark of the LLD.
const outboundQueueSize = 512

// Conn wraps a WebSocket connection with the identity established at
// handshake. Closing is idempotent.
type Conn struct {
	deviceID  string
	userID    string
	sessionID string
	ws        *websocket.Conn
	outbound  chan []byte
	gate      *replayGate       // live/replay interleave (ws-gateway-lld §3)
	rcoal     *receiptCoalescer // inbound receipt coalescing (DS&A §9)
	pres      *connPresence     // presence subscriptions + fan-out (nil if disabled)
	chans     *connChannels     // channel real-time subscriptions (nil if disabled)
	frameID   atomic.Uint64     // per-connection frame_id counter (tracing only)
	closeOnce sync.Once
}

// nextFrameID returns the next outbound frame_id (monotonic per connection).
func (c *Conn) nextFrameID() uint64 { return c.frameID.Add(1) }

// Close shuts the connection with a code+reason exactly once.
func (c *Conn) Close(code CloseCode, reason string) {
	c.closeOnce.Do(func() {
		// Best-effort: a peer that already vanished makes this error, which
		// is nothing we can act on.
		_ = c.ws.Close(code, reason)
	})
}

// write sends one binary frame with a bounded deadline (the whole protocol
// is binary protobuf — websocket-protocol.md).
func (c *Conn) write(ctx context.Context, data []byte) error {
	wctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return c.ws.Write(wctx, websocket.MessageBinary, data)
}

// DeviceID exposes the connection's authenticated device (observability/tests).
func (c *Conn) DeviceID() string { return c.deviceID }

// Deliver enqueues a payload for the write pump without ever blocking the
// caller (the NATS handler). false = the queue is full: the device is a slow
// consumer and the caller applies the backpressure policy (disconnect; the
// inbox holds the backlog).
func (c *Conn) Deliver(payload []byte) bool {
	select {
	case c.outbound <- payload:
		return true
	default:
		return false
	}
}

// writePump serializes all outbound writes (single writer per connection —
// no write races by construction). It exits when ctx ends or a write fails.
func (c *Conn) writePump(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case payload := <-c.outbound:
			if err := c.write(ctx, payload); err != nil {
				c.Close(websocket.StatusGoingAway, "write failed")
				return
			}
		}
	}
}
