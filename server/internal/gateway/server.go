package gateway

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"net/http"

	"github.com/coder/websocket"

	"github.com/whatsapp-v2/server/internal/auth"
	wsv1 "github.com/whatsapp-v2/server/internal/proto/gen/whatsapp/ws/v1"
)

const (
	handshakeTimeout  = 10 * time.Second
	heartbeatInterval = 30 * time.Second
	heartbeatTimeout  = 10 * time.Second
	maxFrameBytes     = 256 << 10 // 256 KB (media goes via presigned upload, never the WS)
	// rpcTimeout bounds each gateway → core-api call (ws-gateway-lld §2).
	rpcTimeout = 2 * time.Second
)

// errSlowConsumer signals a full outbound queue during replay.
var errSlowConsumer = errors.New("outbound queue full")

// Authorizer decides whether an authenticated identity may hold a connection.
// It rejects revoked devices / suspended accounts (→ 4403). In production
// this is a gRPC call to core-api (the gateway has no database access,
// microservices.md §6); tests use a fake.
type Authorizer interface {
	Authorize(ctx context.Context, ident auth.Identity) (bool, error)
}

// AllowAll authorizes every identity — the interim default until the
// core-api session-check gRPC lands.
type AllowAll struct{}

func (AllowAll) Authorize(context.Context, auth.Identity) (bool, error) { return true, nil }

// Server handles WebSocket connections.
type Server struct {
	reg      *Registry
	verifier auth.TokenVerifier
	authz    Authorizer
	routes   RouteStore
	delivery DeliverySource // nil = no live delivery (some tests)
	chat     ChatClient     // nil = no core-api (some tests): no replay, no send path
	resume   ResumeStore    // nil = resume tokens disabled (some tests)
	podID    string
	routeTTL time.Duration
	log      *slog.Logger

	// allowedOrigins is passed to the WS accept; empty accepts same-origin
	// only (the browser default). Cross-origin hosts are set in production.
	allowedOrigins []string
}

// Config wires a Server.
type Config struct {
	Registry       *Registry
	Verifier       auth.TokenVerifier
	Authorizer     Authorizer
	Routes         RouteStore
	Delivery       DeliverySource
	Chat           ChatClient
	Resume         ResumeStore
	PodID          string
	RouteTTL       time.Duration
	Log            *slog.Logger
	AllowedOrigins []string
}

// NewServer builds a Server, applying sane defaults.
func NewServer(cfg Config) *Server {
	if cfg.Registry == nil {
		cfg.Registry = NewRegistry()
	}
	if cfg.Authorizer == nil {
		cfg.Authorizer = AllowAll{}
	}
	if cfg.RouteTTL == 0 {
		cfg.RouteTTL = 90 * time.Second
	}
	if cfg.Log == nil {
		cfg.Log = slog.Default()
	}
	return &Server{
		reg: cfg.Registry, verifier: cfg.Verifier, authz: cfg.Authorizer,
		routes: cfg.Routes, delivery: cfg.Delivery, chat: cfg.Chat,
		resume: cfg.Resume, podID: cfg.PodID, routeTTL: cfg.RouteTTL,
		log: cfg.Log, allowedOrigins: cfg.AllowedOrigins,
	}
}

// Registry exposes the connection registry (drain, metrics).
func (s *Server) Registry() *Registry { return s.reg }

// Handle is the /v1/ws endpoint: upgrade → authenticate → register →
// replay → serve (ws-gateway-lld §3).
func (s *Server) Handle(w http.ResponseWriter, r *http.Request) {
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: s.allowedOrigins,
	})
	if err != nil {
		return // Accept already wrote the HTTP error
	}
	ws.SetReadLimit(maxFrameBytes)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	ident, hello, ok := s.handshake(ctx, ws)
	if !ok {
		return // handshake closed the connection with the right code
	}

	if err := s.routes.Claim(ctx, ident.DeviceID, s.podID, s.routeTTL); err != nil {
		s.log.Error("route claim failed", "device_id", ident.DeviceID, "err", err)
		_ = ws.Close(websocket.StatusInternalError, "route claim failed")
		return
	}

	willReplay := s.chat != nil
	c := &Conn{
		deviceID: ident.DeviceID, userID: ident.UserID, sessionID: ident.SessionID,
		ws: ws, outbound: make(chan []byte, outboundQueueSize),
		gate: newReplayGate(!willReplay),
	}
	if displaced := s.reg.Add(c); displaced != nil {
		// Close asynchronously: a WebSocket close performs a closing handshake
		// that blocks until the peer acks, and this new connection must not
		// wait on the old client to send its hello_ack.
		go displaced.Close(CloseReplaced, "replaced by a newer connection")
	}

	// Resume: cursors narrow the replay only when the presented token is the
	// device's current one — a stale or foreign token degrades to a full
	// replay, never to skipped messages. The token rotates on every connect.
	cursors, resumeToken := s.applyResume(ctx, ident.DeviceID, hello)

	ack := helloAckFrame(c.nextFrameID(), ident.SessionID, time.Now().UnixMilli(), resumeToken, willReplay)
	if err := c.write(ctx, ack); err != nil {
		s.cleanup(c)
		return
	}

	// Attach the live-delivery feed BEFORE the replay snapshot (lld §3 order):
	// anything accepted after the snapshot arrives live; anything before it is
	// in the replay; the gate dedupes the overlap. Deliveries while fully
	// disconnected are covered by the inbox — never lost.
	var unsubscribe func()
	if s.delivery != nil {
		unsubscribe, err = s.delivery.Subscribe(ident.DeviceID, func(payload []byte) {
			item, derr := decodeDelivery(payload, c.deviceID)
			if derr != nil {
				// Drop, don't disconnect: the item is safe in the inbox and
				// resume replay delivers it. Only the live push is lost.
				s.log.Warn("dropping malformed live delivery", "device_id", c.deviceID, "err", derr)
				return
			}
			deliver, overflow := c.gate.holdLive(item)
			if overflow {
				c.Close(CloseSlowConsumer, "slow consumer — resume to catch up")
				return
			}
			if !deliver {
				return // buffered; the replay flush will order it
			}
			if !c.Deliver(itemsFrame(c.nextFrameID(), []*wsv1.InboxItem{item}, false, false)) {
				// Backpressure policy: the backlog stays in the inbox, not in
				// pod memory. The client reconnects and replays from cursor.
				c.Close(CloseSlowConsumer, "slow consumer — resume to catch up")
			}
		})
		if err != nil {
			s.log.Error("delivery subscribe failed", "device_id", ident.DeviceID, "err", err)
			s.cleanup(c)
			return
		}
	}

	if willReplay {
		go s.replay(ctx, c, cursors)
	}

	s.log.Info("connection established", "device_id", ident.DeviceID, "user_id", ident.UserID)
	s.serve(ctx, c)
	if unsubscribe != nil {
		unsubscribe()
	}
	s.cleanup(c)
}

// handshake reads the hello frame, verifies the JWT, and authorizes the
// identity. On any failure it closes the connection with the appropriate
// code and returns ok=false.
func (s *Server) handshake(ctx context.Context, ws *websocket.Conn) (auth.Identity, *wsv1.Hello, bool) {
	hctx, cancel := context.WithTimeout(ctx, handshakeTimeout)
	defer cancel()

	frame, err := readFrame(hctx, ws)
	if err != nil {
		_ = ws.Close(CloseBadHandshake, "expected hello frame")
		return auth.Identity{}, nil, false
	}
	hello := frame.GetHello()
	if hello == nil || hello.GetAccessJwt() == "" {
		s.sendErrorAndClose(ctx, ws, CloseBadHandshake, "VALIDATION_HANDSHAKE", "malformed hello frame")
		return auth.Identity{}, nil, false
	}

	// The JWT is the identity authority; hello.device_id is advisory.
	ident, err := s.verifier.Verify(hello.GetAccessJwt())
	if err != nil {
		s.sendErrorAndClose(ctx, ws, CloseAuthExpired, "AUTH_TOKEN_EXPIRED", "invalid or expired token")
		return auth.Identity{}, nil, false
	}

	allowed, err := s.authz.Authorize(ctx, ident)
	if err != nil {
		_ = ws.Close(websocket.StatusInternalError, "authorization failed")
		return auth.Identity{}, nil, false
	}
	if !allowed {
		s.sendErrorAndClose(ctx, ws, CloseRevoked, "AUTH_DEVICE_REVOKED", "device revoked or account suspended")
		return auth.Identity{}, nil, false
	}
	return ident, hello, true
}

// applyResume validates the presented resume token (if any) and rotates a
// fresh one. Cursors are honored only on a valid resume; failures degrade to
// full replay and/or an absent token — both safe, never lossy.
func (s *Server) applyResume(ctx context.Context, deviceID string, hello *wsv1.Hello) ([]*wsv1.ConversationCursor, string) {
	if s.resume == nil {
		return nil, ""
	}
	var cursors []*wsv1.ConversationCursor
	if token := hello.GetResumeToken(); token != "" {
		ok, err := s.resume.Validate(ctx, deviceID, token)
		if err != nil {
			s.log.Warn("resume validate failed — full replay", "device_id", deviceID, "err", err)
		}
		if ok {
			cursors = hello.GetLastCursors()
		}
	}
	token, err := s.resume.Rotate(ctx, deviceID, resumeTokenTTL)
	if err != nil {
		// No token in the ack: the client simply cannot resume next time.
		s.log.Warn("resume rotate failed", "device_id", deviceID, "err", err)
		return cursors, ""
	}
	return cursors, token
}

// replay streams the inbox through the outbound queue as replay-marked
// InboxBatch frames, then a replay_complete marker, then flushes the live
// items the gate buffered meanwhile (ws-gateway-lld §3 interleave rule).
func (s *Server) replay(ctx context.Context, c *Conn, cursors []*wsv1.ConversationCursor) {
	err := s.chat.PullInbox(ctx, c.deviceID, cursors, func(items []*wsv1.InboxItem) error {
		c.gate.advance(items)
		if !c.Deliver(itemsFrame(c.nextFrameID(), items, true, false)) {
			return errSlowConsumer
		}
		return nil
	})
	switch {
	case err == nil:
	case errors.Is(err, errSlowConsumer):
		c.Close(CloseSlowConsumer, "slow consumer — resume to catch up")
		return
	case ctx.Err() != nil:
		return // connection is gone; reconnect replays
	default:
		s.log.Error("inbox replay failed", "device_id", c.deviceID, "err", err)
		c.Close(websocket.StatusInternalError, "replay failed — reconnect")
		return
	}

	if !c.Deliver(itemsFrame(c.nextFrameID(), nil, true, true)) {
		c.Close(CloseSlowConsumer, "slow consumer — resume to catch up")
		return
	}
	ok := c.gate.finish(func(items []*wsv1.InboxItem) bool {
		return c.Deliver(itemsFrame(c.nextFrameID(), items, false, false))
	})
	if !ok {
		c.Close(CloseSlowConsumer, "slow consumer — resume to catch up")
	}
}

// serve runs the write pump, heartbeat, and the read loop dispatching client
// frames until the connection closes.
func (s *Server) serve(ctx context.Context, c *Conn) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	go c.writePump(ctx)
	go s.heartbeat(ctx, c)

	for {
		frame, err := readFrame(ctx, c.ws)
		if err != nil {
			return // closed, timed out, or protocol violation — cleanup follows
		}
		switch body := frame.GetBody().(type) {
		case *wsv1.Frame_Ping:
			if !c.Deliver(pongFrame(c.nextFrameID(), body.Ping.GetTsMs())) {
				c.Close(CloseSlowConsumer, "slow consumer — resume to catch up")
				return
			}
		case *wsv1.Frame_MsgSend:
			s.handleMsgSend(ctx, c, body.MsgSend)
		case *wsv1.Frame_ClientAck:
			s.handleClientAck(ctx, c, body.ClientAck)
		default:
			// Receipts, presence, typing, … arrive with their tasks
			// (T0.14/T0.15). Unknown frames are ignored, not fatal: old
			// servers must tolerate newer clients.
			s.log.Debug("ignoring frame", "device_id", c.deviceID)
		}
	}
}

// handleMsgSend forwards the send to core-api with the connection's
// authenticated identity and relays the ack (or the typed rejection — the
// connection stays open; rejections are the client's problem).
func (s *Server) handleMsgSend(ctx context.Context, c *Conn, msg *wsv1.MsgSend) {
	if s.chat == nil {
		s.log.Warn("msg_send with no chat backend", "device_id", c.deviceID)
		return
	}
	rctx, cancel := context.WithTimeout(ctx, rpcTimeout)
	ack, werr, err := s.chat.AcceptMessage(rctx, c.userID, c.deviceID, msg)
	cancel()

	var out []byte
	switch {
	case err != nil:
		// Transport failure: tell the client to retry (its outbox owns
		// resending unacked frames — websocket-protocol.md §1).
		s.log.Warn("accept rpc failed", "device_id", c.deviceID, "err", err)
		out = errorFrame("TRANSIENT_UNAVAILABLE", "temporarily unavailable")
	case werr != nil:
		out = wireErrorFrame(c.nextFrameID(), werr)
	default:
		out = msgAckFrame(c.nextFrameID(), ack)
	}
	if !c.Deliver(out) {
		c.Close(CloseSlowConsumer, "slow consumer — resume to catch up")
	}
}

// handleClientAck forwards the cumulative persistence watermark. Failures
// are logged, not surfaced: acks are idempotent and the rows simply replay
// on the next resume until a later ack lands.
func (s *Server) handleClientAck(ctx context.Context, c *Conn, ack *wsv1.ClientAck) {
	if s.chat == nil || len(ack.GetUpTo()) == 0 {
		return
	}
	rctx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()
	if err := s.chat.AckDelivered(rctx, c.deviceID, ack.GetUpTo()); err != nil {
		s.log.Warn("client ack failed", "device_id", c.deviceID, "err", err)
	}
}

// heartbeat pings the peer and keeps the route TTL alive. A failed ping or a
// lost route ownership tears the connection down.
func (s *Server) heartbeat(ctx context.Context, c *Conn) {
	t := time.NewTicker(heartbeatInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			pctx, cancel := context.WithTimeout(ctx, heartbeatTimeout)
			err := c.ws.Ping(pctx)
			cancel()
			if err != nil {
				c.Close(websocket.StatusGoingAway, "heartbeat timeout")
				return
			}
			owned, err := s.routes.Refresh(ctx, c.deviceID, s.podID, s.routeTTL)
			if err == nil && !owned {
				// A newer connection on another pod claimed this device.
				c.Close(CloseReplaced, "replaced by a newer connection")
				return
			}
		}
	}
}

// cleanup removes the connection from the registry and releases its route —
// but only if it is still the current owner (a displaced connection must not
// evict the newer one's registry entry or route).
func (s *Server) cleanup(c *Conn) {
	if s.reg.Remove(c) {
		_ = s.routes.Release(context.Background(), c.deviceID, s.podID)
	}
	c.Close(websocket.StatusNormalClosure, "")
	s.log.Info("connection closed", "device_id", c.deviceID, "user_id", c.userID)
}

func (s *Server) sendErrorAndClose(ctx context.Context, ws *websocket.Conn, code CloseCode, errCode, msg string) {
	wctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_ = ws.Write(wctx, websocket.MessageBinary, errorFrame(errCode, msg))
	_ = ws.Close(code, msg)
}
