package adapters

import (
	"context"
	"fmt"

	"github.com/nats-io/nats.go"
	"google.golang.org/protobuf/proto"

	"github.com/whatsapp-v2/server/internal/presence"
	wsv1 "github.com/whatsapp-v2/server/internal/proto/gen/whatsapp/ws/v1"
)

// PresenceSubject / TypingSubject are the per-user fan-out subjects. A
// subscriber to user U receives U's presence on pres.{U} and U's typing on
// typ.{U} — one fan-out dimension per tracked user (DS&A §10).
func PresenceSubject(userID string) string { return "pres." + userID }
func TypingSubject(userID string) string   { return "typ." + userID }

// NATSPresence implements presence.Publisher and provides the per-user
// subscribe helpers the gateway uses. Presence/typing are ephemeral — core
// NATS pub/sub, no JetStream (nothing to replay).
type NATSPresence struct{ nc *nats.Conn }

func NewNATSPresence(nc *nats.Conn) *NATSPresence { return &NATSPresence{nc: nc} }

func (p *NATSPresence) PublishUpdate(_ context.Context, u presence.Update) error {
	payload, err := proto.Marshal(&wsv1.PresenceUpdate{
		UserId:     u.UserID,
		Online:     u.Online,
		LastSeenMs: u.LastSeenMS,
	})
	if err != nil {
		return fmt.Errorf("encoding presence update: %w", err)
	}
	if err := p.nc.Publish(PresenceSubject(u.UserID), payload); err != nil {
		return fmt.Errorf("publishing presence: %w", err)
	}
	return nil
}

func (p *NATSPresence) PublishTyping(_ context.Context, t presence.TypingEvent) error {
	payload, err := proto.Marshal(&wsv1.Typing{
		ConversationId: t.ConversationID,
		UserId:         t.TyperUserID,
		Recording:      t.Recording,
	})
	if err != nil {
		return fmt.Errorf("encoding typing: %w", err)
	}
	if err := p.nc.Publish(TypingSubject(t.TyperUserID), payload); err != nil {
		return fmt.Errorf("publishing typing: %w", err)
	}
	return nil
}

// SubscribePresence forwards a tracked user's presence payloads to deliver,
// returning an unsubscribe func.
func (p *NATSPresence) SubscribePresence(userID string, deliver func([]byte)) (func(), error) {
	sub, err := p.nc.Subscribe(PresenceSubject(userID), func(m *nats.Msg) { deliver(m.Data) })
	if err != nil {
		return nil, fmt.Errorf("subscribing presence for %s: %w", userID, err)
	}
	return func() { _ = sub.Unsubscribe() }, nil
}

// SubscribeTyping forwards a tracked user's typing payloads to deliver.
func (p *NATSPresence) SubscribeTyping(userID string, deliver func([]byte)) (func(), error) {
	sub, err := p.nc.Subscribe(TypingSubject(userID), func(m *nats.Msg) { deliver(m.Data) })
	if err != nil {
		return nil, fmt.Errorf("subscribing typing for %s: %w", userID, err)
	}
	return func() { _ = sub.Unsubscribe() }, nil
}

var _ presence.Publisher = (*NATSPresence)(nil)
