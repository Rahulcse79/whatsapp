package adapters

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/nats-io/nats.go"
)

// channelSubject is the per-channel broadcast subject. The WS gateway subscribes
// (channel.{id}.post) and forwards a lightweight "new post" nudge to the
// devices of online followers, which then pull the post. This is the real-time
// seam — the durable path is followers pulling ListPosts, so a dropped nudge
// only costs immediacy, not the post.
func channelSubject(channelID string) string { return "channel." + channelID + ".post" }

// NATSBroadcaster implements channels.Broadcaster over core NATS. Best-effort:
// the post already committed to PostgreSQL (the source of truth) before the
// nudge, so a publish failure never loses the post.
type NATSBroadcaster struct {
	nc  *nats.Conn
	log *slog.Logger
}

func NewNATSBroadcaster(nc *nats.Conn, log *slog.Logger) *NATSBroadcaster {
	return &NATSBroadcaster{nc: nc, log: log}
}

func (b *NATSBroadcaster) PostPublished(_ context.Context, channelID, postID string) error {
	payload, _ := json.Marshal(map[string]any{"kind": "post", "channel": channelID, "post": postID})
	if err := b.nc.Publish(channelSubject(channelID), payload); err != nil {
		if b.log != nil {
			b.log.Warn("channels: publishing post nudge failed (best-effort)", "channel", channelID, "post", postID, "err", err)
		}
		return err
	}
	return nil
}
