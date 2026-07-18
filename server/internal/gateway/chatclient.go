package gateway

import (
	"context"

	commonv1 "github.com/whatsapp-v2/server/internal/proto/gen/whatsapp/common/v1"
	wsv1 "github.com/whatsapp-v2/server/internal/proto/gen/whatsapp/ws/v1"
)

// ChatClient is the gateway's port to core-api's ChatService (gRPC in
// production — adapters/grpc.go; fakes in tests). The gateway is a smart
// pipe: sender identity comes from the authenticated connection, never from
// the frame (rpc.proto).
type ChatClient interface {
	// AcceptMessage runs the send hot path. A domain rejection comes back as
	// the typed error (connection stays open); err is transport-level only.
	AcceptMessage(ctx context.Context, senderUserID, senderDeviceID string, msg *wsv1.MsgSend) (*wsv1.MsgAck, *commonv1.Error, error)

	// PullInbox streams the device's pending inbox in (conversation, seq)
	// order; emit is called once per non-empty batch until the replay is
	// drained. An emit error aborts the pull.
	PullInbox(ctx context.Context, deviceID string, cursors []*wsv1.ConversationCursor, emit func(items []*wsv1.InboxItem) error) error

	// AckDelivered forwards the client's cumulative persistence watermark
	// (ACK-after-persist); the server deletes covered inbox rows.
	AckDelivered(ctx context.Context, deviceID string, upTo []*wsv1.ConversationCursor) error
}
