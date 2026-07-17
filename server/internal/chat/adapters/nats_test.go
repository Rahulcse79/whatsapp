package adapters

import (
	"bytes"
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/whatsapp-v2/server/internal/chat"
	eventsv1 "github.com/whatsapp-v2/server/internal/proto/gen/whatsapp/events/v1"
	wsv1 "github.com/whatsapp-v2/server/internal/proto/gen/whatsapp/ws/v1"
)

func TestDeliverySubject(t *testing.T) {
	if got, want := DeliverySubject("dev-1"), "dev.dev-1.out"; got != want {
		t.Fatalf("subject = %q, want %q", got, want)
	}
}

func TestEncodeDelivery_RoundTrip(t *testing.T) {
	payload, err := encodeDelivery("recipient-dev", chat.InboxItem{
		ConversationID: "conv-1",
		Seq:            42,
		MsgUUID:        "msg-1",
		SenderUserID:   "u-sender",
		SenderDeviceID: "d-sender",
		Kind:           chat.KindReaction,
		OverlayTarget:  "msg-0",
		Ciphertext:     []byte{0xde, 0xad},
		AcceptedAtMS:   1234,
	})
	if err != nil {
		t.Fatalf("encodeDelivery: %v", err)
	}

	d := &eventsv1.Delivery{}
	if err := proto.Unmarshal(payload, d); err != nil {
		t.Fatalf("payload is not events.v1.Delivery: %v", err)
	}
	if d.GetRecipientDeviceId() != "recipient-dev" {
		t.Fatalf("recipient = %q", d.GetRecipientDeviceId())
	}
	item := d.GetItem()
	switch {
	case item.GetConversationId() != "conv-1",
		item.GetSeq() != 42,
		item.GetMsgUuid() != "msg-1",
		item.GetSenderUserId() != "u-sender",
		item.GetSenderDeviceId() != "d-sender",
		item.GetKind() != wsv1.MsgKind_MSG_KIND_REACTION,
		item.GetOverlayTarget() != "msg-0",
		!bytes.Equal(item.GetSealedEnvelope(), []byte{0xde, 0xad}),
		item.GetAcceptedAtMs() != 1234:
		t.Fatalf("item fields did not round-trip: %v", item)
	}
}
