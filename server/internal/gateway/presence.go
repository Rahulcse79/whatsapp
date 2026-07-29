package gateway

import (
	"context"
	"sync"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/whatsapp-v2/server/internal/presence"
	wsv1 "github.com/whatsapp-v2/server/internal/proto/gen/whatsapp/ws/v1"
)

// PresenceBackend is the gateway's port to the presence subsystem: online-state
// tracking (Valkey), fan-out (NATS pres.{u}/typ.{u}), and subscribe-time
// privacy. Presence/typing carry no business rules and are latency-sensitive,
// so the gateway drives them directly rather than through core-api gRPC
// (ws-gateway-lld §2). In production it's the composite of the presence Store,
// the NATS publisher, and a PrivacyChecker; tests use a fake.
type PresenceBackend interface {
	Connect(ctx context.Context, userID, deviceID string, nowMS int64) (becameOnline bool, err error)
	Heartbeat(ctx context.Context, userID, deviceID string, nowMS int64) error
	Disconnect(ctx context.Context, userID, deviceID string, nowMS int64) (becameOffline bool, err error)
	Snapshot(ctx context.Context, userID string, nowMS int64) (presence.Update, error)
	PublishUpdate(ctx context.Context, u presence.Update) error
	PublishTyping(ctx context.Context, t presence.TypingEvent) error
	CanSeePresence(ctx context.Context, viewerUserID, targetUserID string) (bool, error)
	SubscribePresence(userID string, deliver func([]byte)) (func(), error)
	SubscribeTyping(userID string, deliver func([]byte)) (func(), error)
}

// connPresence is a connection's presence state: which users it tracks and the
// live fan-out subscriptions backing them.
type connPresence struct {
	subs      *presence.Subscriptions
	connected bool // presenceConnect ran — guards a spurious offline on early exits
	mu        sync.Mutex
	unsub     map[string]func() // target user → combined presence+typing unsubscribe
}

func newConnPresence() *connPresence {
	return &connPresence{subs: presence.NewSubscriptions(), unsub: make(map[string]func())}
}

func (p *connPresence) closeAll() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for u, f := range p.unsub {
		f()
		delete(p.unsub, u)
	}
	p.subs.Clear()
}

func flapKey(userID, deviceID string) string { return userID + ":" + deviceID }

// presenceConnect marks the connection's user online (cancelling any pending
// flap offline) and fans out the offline→online transition.
func (s *Server) presenceConnect(ctx context.Context, c *Conn) {
	c.pres.connected = true
	s.flap.Reconnected(flapKey(c.userID, c.deviceID))
	now := time.Now().UnixMilli()
	online, err := s.presence.Connect(ctx, c.userID, c.deviceID, now)
	if err != nil {
		s.log.Warn("presence connect failed", "device_id", c.deviceID, "err", err)
		return
	}
	if online {
		if err := s.presence.PublishUpdate(ctx, presence.Update{UserID: c.userID, Online: true, LastSeenMS: now}); err != nil {
			s.log.Warn("presence publish (online) failed", "user_id", c.userID, "err", err)
		}
	}
}

// presenceDisconnect closes the connection's fan-out and schedules a
// flap-damped offline for its device.
func (s *Server) presenceDisconnect(c *Conn) {
	c.pres.closeAll()
	s.flap.Disconnected(flapKey(c.userID, c.deviceID), func() {
		ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
		defer cancel()
		now := time.Now().UnixMilli()
		offline, err := s.presence.Disconnect(ctx, c.userID, c.deviceID, now)
		if err != nil {
			s.log.Warn("presence disconnect failed", "device_id", c.deviceID, "err", err)
			return
		}
		if offline {
			if err := s.presence.PublishUpdate(ctx, presence.Update{UserID: c.userID, Online: false, LastSeenMS: now}); err != nil {
				s.log.Warn("presence publish (offline) failed", "user_id", c.userID, "err", err)
			}
		}
	})
}

// handlePresenceSub applies a subscription delta and opens/closes fan-out.
func (s *Server) handlePresenceSub(ctx context.Context, c *Conn, f *wsv1.PresenceSub) {
	added, removed := c.pres.subs.Apply(f.GetSubscribeUserIds(), f.GetUnsubscribeUserIds())
	for _, u := range removed {
		s.closePresenceFeed(c, u)
	}
	for _, u := range added {
		s.openPresenceFeed(ctx, c, u)
	}
}

// openPresenceFeed enforces subscribe-time privacy, wires the NATS fan-out for
// one target, and pushes the current snapshot. Any failure revokes the
// subscription so state stays consistent.
func (s *Server) openPresenceFeed(ctx context.Context, c *Conn, target string) {
	if ok, err := s.presence.CanSeePresence(ctx, c.userID, target); err != nil || !ok {
		c.pres.subs.Apply(nil, []string{target})
		return
	}
	unsubPres, err := s.presence.SubscribePresence(target, func(payload []byte) {
		if !c.pres.subs.Has(target) {
			return
		}
		u := &wsv1.PresenceUpdate{}
		if proto.Unmarshal(payload, u) != nil {
			return
		}
		if !c.Deliver(presenceUpdateFrame(c.nextFrameID(), u)) {
			s.log.Debug("dropping presence on full queue", "device_id", c.deviceID)
		}
	})
	if err != nil {
		c.pres.subs.Apply(nil, []string{target})
		return
	}
	unsubTyp, err := s.presence.SubscribeTyping(target, func(payload []byte) {
		if !c.pres.subs.Has(target) {
			return
		}
		tp := &wsv1.Typing{}
		if proto.Unmarshal(payload, tp) != nil {
			return
		}
		if !c.Deliver(typingFrame(c.nextFrameID(), tp)) {
			s.log.Debug("dropping typing on full queue", "device_id", c.deviceID)
		}
	})
	if err != nil {
		unsubPres()
		c.pres.subs.Apply(nil, []string{target})
		return
	}

	c.pres.mu.Lock()
	c.pres.unsub[target] = func() { unsubPres(); unsubTyp() }
	c.pres.mu.Unlock()

	// Initial state so the client isn't blind until the next change.
	if snap, err := s.presence.Snapshot(ctx, target, time.Now().UnixMilli()); err == nil {
		c.Deliver(presenceUpdateFrame(c.nextFrameID(), &wsv1.PresenceUpdate{
			UserId: snap.UserID, Online: snap.Online, LastSeenMs: snap.LastSeenMS,
		}))
	}
}

func (s *Server) closePresenceFeed(c *Conn, target string) {
	c.pres.mu.Lock()
	if f, ok := c.pres.unsub[target]; ok {
		f()
		delete(c.pres.unsub, target)
	}
	c.pres.mu.Unlock()
}

// handleTyping publishes the sender's typing indicator on their per-user
// subject; subscribers render it against the matching open conversation.
func (s *Server) handleTyping(ctx context.Context, c *Conn, f *wsv1.Typing) {
	if f.GetConversationId() == "" {
		return
	}
	if err := s.presence.PublishTyping(ctx, presence.TypingEvent{
		TyperUserID:    c.userID,
		ConversationID: f.GetConversationId(),
		Recording:      f.GetRecording(),
	}); err != nil {
		s.log.Debug("typing publish failed", "device_id", c.deviceID, "err", err)
	}
}
