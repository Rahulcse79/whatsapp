package gateway

// Binary protobuf frame codec (websocket-protocol.md): every message on the
// wire is one wsv1.Frame envelope per WebSocket binary message. This file is
// the only place gateway code touches the encoding, so protocol changes stay
// a contained diff.

import (
	"context"
	"fmt"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/proto"

	commonv1 "github.com/whatsapp-v2/server/internal/proto/gen/whatsapp/common/v1"
	eventsv1 "github.com/whatsapp-v2/server/internal/proto/gen/whatsapp/events/v1"
	wsv1 "github.com/whatsapp-v2/server/internal/proto/gen/whatsapp/ws/v1"
)

// readFrame reads one WebSocket message and decodes the frame envelope. Text
// messages are a protocol violation: the contract is binary protobuf only.
func readFrame(ctx context.Context, ws *websocket.Conn) (*wsv1.Frame, error) {
	typ, data, err := ws.Read(ctx)
	if err != nil {
		return nil, err
	}
	if typ != websocket.MessageBinary {
		return nil, fmt.Errorf("frames are binary websocket messages, got type %d", typ)
	}
	f := &wsv1.Frame{}
	if err := proto.Unmarshal(data, f); err != nil {
		return nil, fmt.Errorf("decoding frame: %w", err)
	}
	return f, nil
}

// errorFrame encodes the uniform error envelope sent before an abnormal
// close (api-standards.md §2).
func errorFrame(code, msg string) []byte {
	return wireErrorFrame(0, &commonv1.Error{Code: code, Message: msg})
}

// wireErrorFrame encodes an already-typed domain error (e.g. a core-api
// rejection relayed to the sender; the connection stays open).
func wireErrorFrame(frameID uint64, werr *commonv1.Error) []byte {
	payload, _ := proto.Marshal(&wsv1.Frame{
		FrameId: frameID,
		Body:    &wsv1.Frame_Error{Error: werr},
	})
	return payload
}

// helloAckFrame encodes the handshake acknowledgement: the rotated resume
// token and whether an inbox replay will precede live traffic.
func helloAckFrame(frameID uint64, sessionID string, serverTimeMS int64, resumeToken string, replayPending bool) []byte {
	payload, _ := proto.Marshal(&wsv1.Frame{
		FrameId: frameID,
		Body: &wsv1.Frame_HelloAck{HelloAck: &wsv1.HelloAck{
			ResumeToken:   resumeToken,
			SessionId:     sessionID,
			ServerTimeMs:  serverTimeMS,
			ReplayPending: replayPending,
		}},
	})
	return payload
}

// pongFrame answers an application-level Ping.
func pongFrame(frameID uint64, echoTSMS int64) []byte {
	payload, _ := proto.Marshal(&wsv1.Frame{
		FrameId: frameID,
		Body:    &wsv1.Frame_Pong{Pong: &wsv1.Pong{EchoTsMs: echoTSMS}},
	})
	return payload
}

// msgAckFrame relays the "sent" acknowledgement for a MsgSend.
func msgAckFrame(frameID uint64, ack *wsv1.MsgAck) []byte {
	payload, _ := proto.Marshal(&wsv1.Frame{
		FrameId: frameID,
		Body:    &wsv1.Frame_MsgAck{MsgAck: ack},
	})
	return payload
}

// itemsFrame encodes an InboxBatch frame — replay batches (replay=true, the
// final one also replay_complete=true) and live deliveries (both false).
func itemsFrame(frameID uint64, items []*wsv1.InboxItem, replay, replayComplete bool) []byte {
	payload, _ := proto.Marshal(&wsv1.Frame{
		FrameId: frameID,
		Body: &wsv1.Frame_InboxBatch{InboxBatch: &wsv1.InboxBatch{
			Items:          items,
			Replay:         replay,
			ReplayComplete: replayComplete,
		}},
	})
	return payload
}

// decodeDelivery decodes one NATS delivery payload (events.v1.Delivery,
// published by chat/adapters). deviceID guards against subject/payload skew —
// a delivery addressed elsewhere is a bug, not an item to forward.
func decodeDelivery(payload []byte, deviceID string) (*wsv1.InboxItem, error) {
	d := &eventsv1.Delivery{}
	if err := proto.Unmarshal(payload, d); err != nil {
		return nil, fmt.Errorf("decoding delivery: %w", err)
	}
	if d.GetItem() == nil {
		return nil, fmt.Errorf("delivery without item")
	}
	if rid := d.GetRecipientDeviceId(); rid != "" && rid != deviceID {
		return nil, fmt.Errorf("delivery addressed to %q arrived on %q's subject", rid, deviceID)
	}
	return d.GetItem(), nil
}
