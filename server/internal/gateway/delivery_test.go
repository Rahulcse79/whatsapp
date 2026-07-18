package gateway

import (
	"fmt"
	"testing"
	"time"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/proto"

	eventsv1 "github.com/whatsapp-v2/server/internal/proto/gen/whatsapp/events/v1"
	wsv1 "github.com/whatsapp-v2/server/internal/proto/gen/whatsapp/ws/v1"
)

// deliveryPayload builds the events.v1.Delivery bytes that chat/adapters
// publishes on dev.{device}.out.
func deliveryPayload(t *testing.T, deviceID, msgUUID string, seq int64) []byte {
	t.Helper()
	payload, err := proto.Marshal(&eventsv1.Delivery{
		RecipientDeviceId: deviceID,
		Item: &wsv1.InboxItem{
			ConversationId: "c1",
			Seq:            seq,
			MsgUuid:        msgUUID,
			Kind:           wsv1.MsgKind_MSG_KIND_TEXT,
		},
	})
	if err != nil {
		t.Fatalf("marshal delivery: %v", err)
	}
	return payload
}

// expectInboxItem reads one frame and unwraps the single live inbox item.
func expectInboxItem(t *testing.T, c *websocket.Conn) *wsv1.InboxItem {
	t.Helper()
	f := readServerFrame(t, c)
	batch := f.GetInboxBatch()
	if batch == nil {
		t.Fatalf("not an inbox_batch: %v", f)
	}
	if batch.GetReplay() {
		t.Fatal("live delivery must not be flagged as replay")
	}
	if len(batch.GetItems()) != 1 {
		t.Fatalf("live batch has %d items, want 1", len(batch.GetItems()))
	}
	return batch.GetItems()[0]
}

func TestDeliver_QueueOverflowReportsFalse(t *testing.T) {
	c := &Conn{deviceID: "d1", outbound: make(chan []byte, 2)}
	if !c.Deliver([]byte("1")) || !c.Deliver([]byte("2")) {
		t.Fatal("first two deliveries must fit the queue")
	}
	if c.Deliver([]byte("3")) {
		t.Fatal("third delivery must overflow — pod memory is bounded")
	}
	// Nothing beyond the cap is buffered: the backlog's home is the inbox.
	if len(c.outbound) != 2 {
		t.Fatalf("queue holds %d, want exactly 2", len(c.outbound))
	}
}

func TestDecodeDelivery_RoundTrip(t *testing.T) {
	item, err := decodeDelivery(deliveryPayload(t, "d1", "m1", 7), "d1")
	if err != nil {
		t.Fatalf("decodeDelivery: %v", err)
	}
	if item.GetSeq() != 7 || item.GetMsgUuid() != "m1" {
		t.Fatalf("unexpected item: %v", item)
	}
	frame := itemsFrame(42, []*wsv1.InboxItem{item}, false, false)
	f := &wsv1.Frame{}
	if err := proto.Unmarshal(frame, f); err != nil {
		t.Fatalf("unmarshal frame: %v", err)
	}
	if f.GetFrameId() != 42 {
		t.Fatalf("frame_id = %d, want 42", f.GetFrameId())
	}
	items := f.GetInboxBatch().GetItems()
	if len(items) != 1 || items[0].GetSeq() != 7 || items[0].GetMsgUuid() != "m1" {
		t.Fatalf("unexpected batch: %v", f.GetInboxBatch())
	}
}

func TestDecodeDelivery_RejectsGarbageAndSkew(t *testing.T) {
	if _, err := decodeDelivery([]byte("not-a-proto"), "d1"); err == nil {
		t.Fatal("garbage payload must not become an item")
	}
	if _, err := decodeDelivery(deliveryPayload(t, "other-device", "m1", 1), "d1"); err == nil {
		t.Fatal("delivery addressed to another device must be dropped")
	}
	empty, _ := proto.Marshal(&eventsv1.Delivery{RecipientDeviceId: "d1"})
	if _, err := decodeDelivery(empty, "d1"); err == nil {
		t.Fatal("delivery without an item must be dropped")
	}
}

func TestDelivery_LiveForwarding(t *testing.T) {
	source := NewMemoryDeliverySource()
	hs, issuer, _ := newTestServerWithDelivery(t, AllowAll{}, source)
	tok, _ := issuer.Issue("u1", "dlv1", "sess1")

	c := dial(t, hs)
	sendHello(t, c, tok)
	expectAck(t, c)

	source.Publish("dlv1", deliveryPayload(t, "dlv1", "m7", 7))

	item := expectInboxItem(t, c)
	if item.GetConversationId() != "c1" || item.GetSeq() != 7 {
		t.Fatalf("delivered item = %v, want c1/seq 7", item)
	}
	_ = c.Close(websocket.StatusNormalClosure, "")
}

func TestDelivery_UnsubscribedAfterDisconnect(t *testing.T) {
	source := NewMemoryDeliverySource()
	hs, issuer, s := newTestServerWithDelivery(t, AllowAll{}, source)
	tok, _ := issuer.Issue("u1", "gone1", "sess1")

	c := dial(t, hs)
	sendHello(t, c, tok)
	expectAck(t, c)
	_ = c.Close(websocket.StatusNormalClosure, "")

	// Wait until the server observed the close and cleaned up.
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, ok := s.Registry().Get("gone1"); !ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("connection never left the registry")
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Publishing to a disconnected device must be a no-op (handler removed);
	// the message's home is the inbox, picked up by replay.
	source.mu.Lock()
	_, subscribed := source.subs["gone1"]
	source.mu.Unlock()
	if subscribed {
		t.Fatal("delivery subscription leaked after disconnect")
	}
}

func TestDelivery_ManyMessagesInOrder(t *testing.T) {
	source := NewMemoryDeliverySource()
	hs, issuer, _ := newTestServerWithDelivery(t, AllowAll{}, source)
	tok, _ := issuer.Issue("u1", "ord1", "sess1")

	c := dial(t, hs)
	sendHello(t, c, tok)
	expectAck(t, c)

	const n = 50
	for i := 0; i < n; i++ {
		source.Publish("ord1", deliveryPayload(t, "ord1", fmt.Sprintf("m%03d", i), int64(i)))
	}
	for i := 0; i < n; i++ {
		item := expectInboxItem(t, c)
		if want := fmt.Sprintf("m%03d", i); item.GetMsgUuid() != want {
			t.Fatalf("out of order: got %s, want %s", item.GetMsgUuid(), want)
		}
	}
	_ = c.Close(websocket.StatusNormalClosure, "")
}
