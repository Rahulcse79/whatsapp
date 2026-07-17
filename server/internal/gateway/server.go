package gateway

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"net/http"

	"github.com/coder/websocket"

	"github.com/whatsapp-v2/server/internal/auth"
)

const (
	handshakeTimeout  = 10 * time.Second
	heartbeatInterval = 30 * time.Second
	heartbeatTimeout  = 10 * time.Second
	maxFrameBytes     = 256 << 10 // 256 KB (media goes via presigned upload, never the WS)
)

// Authorizer decides whether an authenticated identity may hold a connection.
// It rejects revoked devices / suspended accounts (→ 4403). In production
// this is a gRPC call to core-api (the gateway has no database access,
// microservices.md §6); tests use a fake.
type Authorizer interface {
	Authorize(ctx context.Context, ident auth.Identity) (bool, error)
}

// AllowAll authorizes every identity — the interim default until the
// core-api session-check gRPC lands (T0.11/T0.12).
type AllowAll struct{}

func (AllowAll) Authorize(context.Context, auth.Identity) (bool, error) { return true, nil }

// Server handles WebSocket connections.
type Server struct {
	reg      *Registry
	verifier auth.TokenVerifier
	authz    Authorizer
	routes   RouteStore
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
		routes: cfg.Routes, podID: cfg.PodID, routeTTL: cfg.RouteTTL,
		log: cfg.Log, allowedOrigins: cfg.AllowedOrigins,
	}
}

// Registry exposes the connection registry (drain, metrics).
func (s *Server) Registry() *Registry { return s.reg }

// Handle is the /v1/ws endpoint: upgrade → authenticate → register → serve.
func (s *Server) Handle(w http.ResponseWriter, r *http.Request) {
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: s.allowedOrigins,
	})
	if err != nil {
		return // Accept already wrote the HTTP error
	}
	ws.SetReadLimit(maxFrameBytes)

	ident, ok := s.handshake(r.Context(), ws)
	if !ok {
		return // handshake closed the connection with the right code
	}

	if err := s.routes.Claim(r.Context(), ident.DeviceID, s.podID, s.routeTTL); err != nil {
		s.log.Error("route claim failed", "device_id", ident.DeviceID, "err", err)
		_ = ws.Close(websocket.StatusInternalError, "route claim failed")
		return
	}

	c := &Conn{deviceID: ident.DeviceID, userID: ident.UserID, sessionID: ident.SessionID, ws: ws}
	if displaced := s.reg.Add(c); displaced != nil {
		// Close asynchronously: a WebSocket close performs a closing handshake
		// that blocks until the peer acks, and this new connection must not
		// wait on the old client to send its hello_ack.
		go displaced.Close(CloseReplaced, "replaced by a newer connection")
	}

	ack, _ := json.Marshal(helloAckFrame{
		Type: frameHelloAck, SessionID: ident.SessionID, ServerTimeMS: time.Now().UnixMilli(),
	})
	if err := c.write(r.Context(), ack); err != nil {
		s.cleanup(c)
		return
	}

	s.log.Info("connection established", "device_id", ident.DeviceID, "user_id", ident.UserID)
	s.serve(r.Context(), c)
	s.cleanup(c)
}

// handshake reads the hello frame, verifies the JWT, and authorizes the
// identity. On any failure it closes the connection with the appropriate
// code and returns ok=false.
func (s *Server) handshake(ctx context.Context, ws *websocket.Conn) (auth.Identity, bool) {
	hctx, cancel := context.WithTimeout(ctx, handshakeTimeout)
	defer cancel()

	_, data, err := ws.Read(hctx)
	if err != nil {
		_ = ws.Close(CloseBadHandshake, "expected hello frame")
		return auth.Identity{}, false
	}
	var hello helloFrame
	if err := json.Unmarshal(data, &hello); err != nil || hello.Type != frameHello || hello.AccessJWT == "" {
		s.sendErrorAndClose(ctx, ws, CloseBadHandshake, "VALIDATION_HANDSHAKE", "malformed hello frame")
		return auth.Identity{}, false
	}

	ident, err := s.verifier.Verify(hello.AccessJWT)
	if err != nil {
		s.sendErrorAndClose(ctx, ws, CloseAuthExpired, "AUTH_TOKEN_EXPIRED", "invalid or expired token")
		return auth.Identity{}, false
	}

	allowed, err := s.authz.Authorize(ctx, ident)
	if err != nil {
		_ = ws.Close(websocket.StatusInternalError, "authorization failed")
		return auth.Identity{}, false
	}
	if !allowed {
		s.sendErrorAndClose(ctx, ws, CloseRevoked, "AUTH_DEVICE_REVOKED", "device revoked or account suspended")
		return auth.Identity{}, false
	}
	return ident, true
}

// serve runs the heartbeat and read loop until the connection closes.
func (s *Server) serve(ctx context.Context, c *Conn) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	go s.heartbeat(ctx, c)

	// Skeleton read loop: drain frames to detect close and honor client
	// activity. Frame routing to core-api lands with T0.11/T0.12.
	for {
		_, _, err := c.ws.Read(ctx)
		if err != nil {
			return
		}
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
	payload, _ := json.Marshal(errorFrame{Type: frameError, Code: errCode, Message: msg})
	wctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_ = ws.Write(wctx, websocket.MessageText, payload)
	_ = ws.Close(code, msg)
}
