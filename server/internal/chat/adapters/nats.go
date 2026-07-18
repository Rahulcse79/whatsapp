package adapters

import (
	"context"
	"fmt"

	"github.com/nats-io/nats.go"
	"google.golang.org/protobuf/proto"

	"github.com/whatsapp-v2/server/internal/chat"
	eventsv1 "github.com/whatsapp-v2/server/internal/proto/gen/whatsapp/events/v1"
	wsv1 "github.com/whatsapp-v2/server/internal/proto/gen/whatsapp/ws/v1"
)

// DeliverySubject is the per-device live-delivery subject
// (internal-events-nats.md: dev.{device_id}.out).
func DeliverySubject(deviceID string) string { return "dev." + deviceID + ".out" }

// NATSPublisher implements chat.Publisher over core NATS publish. The payload
// is the events.v1.Delivery protobuf message (internal-events-nats.md §4).
//
// Deliberate divergence, noted in internal-events-nats.md §1: the DELIVERY
// JetStream stream is not used yet. Durability lives in the PG inbox + resume
// replay; NATS carries only the live push to currently connected devices, for
// which core publish/subscribe is sufficient and far cheaper than 20k
// JetStream consumers. Revisit when redelivery windows are wanted (T0.16
// wires JetStream for PUSH, which genuinely needs it).
type NATSPublisher struct{ nc *nats.Conn }

func NewNATSPublisher(nc *nats.Conn) *NATSPublisher { return &NATSPublisher{nc: nc} }

func (p *NATSPublisher) PublishDelivery(_ context.Context, recipientDeviceID string, item chat.InboxItem) error {
	payload, err := encodeDelivery(recipientDeviceID, item)
	if err != nil {
		return err
	}
	if err := p.nc.Publish(DeliverySubject(recipientDeviceID), payload); err != nil {
		return fmt.Errorf("publishing delivery: %w", err)
	}
	return nil
}

// encodeDelivery marshals the events.v1.Delivery wire message.
func encodeDelivery(recipientDeviceID string, item chat.InboxItem) ([]byte, error) {
	payload, err := proto.Marshal(&eventsv1.Delivery{
		RecipientDeviceId: recipientDeviceID,
		Item:              wireItem(item),
	})
	if err != nil {
		return nil, fmt.Errorf("encoding delivery: %w", err)
	}
	return payload, nil
}

// wireItem converts the domain delivery unit to its wsv1 wire message —
// the single mapping both the NATS and gRPC edges share.
func wireItem(item chat.InboxItem) *wsv1.InboxItem {
	return &wsv1.InboxItem{
		ConversationId: item.ConversationID,
		Seq:            item.Seq,
		MsgUuid:        item.MsgUUID,
		SenderUserId:   item.SenderUserID,
		SenderDeviceId: item.SenderDeviceID,
		// chat.MsgKind mirrors the wire enum value for value (chat.go).
		Kind:           wsv1.MsgKind(item.Kind),
		OverlayTarget:  item.OverlayTarget,
		SealedEnvelope: item.Ciphertext,
		AcceptedAtMs:   item.AcceptedAtMS,
	}
}

var _ chat.Publisher = (*NATSPublisher)(nil)
