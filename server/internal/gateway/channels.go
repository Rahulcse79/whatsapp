package gateway

import (
	"encoding/json"
	"sync"

	wsv1 "github.com/whatsapp-v2/server/internal/proto/gen/whatsapp/ws/v1"
)

// ChannelSource subscribes to a channel's broadcast nudges (channel.{id}.post,
// published by the channels Broadcaster). The gateway forwards a ChannelEvent to
// the followers who declared that channel via ChannelSub; the client then pulls
// the post over REST — the durable path — so a dropped nudge costs immediacy,
// not the post. Mirrors DeliverySource; the NATS-backed impl lives in adapters.
type ChannelSource interface {
	Subscribe(channelID string, deliver func(payload []byte)) (unsubscribe func(), err error)
}

// connChannels tracks a connection's channel subscriptions and their fan-out
// unsubscribes (keyed by channel id). Concurrency-safe: the subscribe delta runs
// on the read loop while nudges fire on NATS callback goroutines.
type connChannels struct {
	mu    sync.Mutex
	unsub map[string]func() // channelID → unsubscribe
}

func newConnChannels() *connChannels { return &connChannels{unsub: make(map[string]func())} }

func (c *connChannels) closeAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for id, f := range c.unsub {
		f()
		delete(c.unsub, id)
	}
}

// channelNudge mirrors the JSON the channels Broadcaster publishes on
// channel.{id}.post (adapters.NATSBroadcaster.PostPublished).
type channelNudge struct {
	Kind    string `json:"kind"`
	Channel string `json:"channel"`
	Post    string `json:"post"`
}

// handleChannelSub applies a follow/unfollow subscription delta: it opens live
// fan-out for newly-followed channels and closes it for unfollowed ones. The
// client sends this on connect (its followed set) and on any follow change.
func (s *Server) handleChannelSub(c *Conn, f *wsv1.ChannelSub) {
	for _, id := range f.GetUnsubscribeChannelIds() {
		s.closeChannelFeed(c, id)
	}
	for _, id := range f.GetSubscribeChannelIds() {
		s.openChannelFeed(c, id)
	}
}

// openChannelFeed subscribes the connection to one channel's nudges (idempotent
// per channel). Each nudge becomes a ChannelEvent the client acts on by pulling.
func (s *Server) openChannelFeed(c *Conn, channelID string) {
	if channelID == "" || c.chans == nil {
		return
	}
	c.chans.mu.Lock()
	if _, ok := c.chans.unsub[channelID]; ok {
		c.chans.mu.Unlock()
		return // already subscribed
	}
	c.chans.mu.Unlock()

	unsub, err := s.channels.Subscribe(channelID, func(payload []byte) {
		var n channelNudge
		if json.Unmarshal(payload, &n) != nil {
			return
		}
		ev := &wsv1.ChannelEvent{ChannelId: n.Channel, PostId: n.Post}
		if !c.Deliver(channelEventFrame(c.nextFrameID(), ev)) {
			s.log.Debug("dropping channel event on full queue", "device_id", c.deviceID)
		}
	})
	if err != nil {
		s.log.Warn("channel subscribe failed", "channel", channelID, "err", err)
		return
	}
	c.chans.mu.Lock()
	c.chans.unsub[channelID] = unsub
	c.chans.mu.Unlock()
}

func (s *Server) closeChannelFeed(c *Conn, channelID string) {
	if c.chans == nil {
		return
	}
	c.chans.mu.Lock()
	if f, ok := c.chans.unsub[channelID]; ok {
		f()
		delete(c.chans.unsub, channelID)
	}
	c.chans.mu.Unlock()
}
