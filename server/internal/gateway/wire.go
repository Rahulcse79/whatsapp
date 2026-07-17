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
	payload, _ := proto.Marshal(&wsv1.Frame{
		Body: &wsv1.Frame_Error{Error: &commonv1.Error{Code: code, Message: msg}},
	})
	return payload
}

// helloAckFrame encodes the handshake acknowledgement. The resume_token /
// replay_pending fields arrive with the session-resume path (T0.13).
func helloAckFrame(frameID uint64, sessionID string, serverTimeMS int64) []byte {
	payload, _ := proto.Marshal(&wsv1.Frame{
		FrameId: frameID,
		Body: &wsv1.Frame_HelloAck{HelloAck: &wsv1.HelloAck{
			SessionId:    sessionID,
			ServerTimeMs: serverTimeMS,
		}},
	})
	return payload
}

// liveInboxFrame transcodes one NATS delivery payload (events.v1.Delivery,
// published by chat/adapters) into the client-facing frame: an InboxBatch of
// a single live item (replay=false). deviceID guards against subject/payload
// skew — a delivery addressed elsewhere is a bug, not a frame to forward.
func liveInboxFrame(payload []byte, frameID uint64, deviceID string) ([]byte, error) {
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
	frame, err := proto.Marshal(&wsv1.Frame{
		FrameId: frameID,
		Body: &wsv1.Frame_InboxBatch{InboxBatch: &wsv1.InboxBatch{
			Items: []*wsv1.InboxItem{d.GetItem()},
		}},
	})
	if err != nil {
		return nil, fmt.Errorf("encoding inbox batch: %w", err)
	}
	return frame, nil
}
