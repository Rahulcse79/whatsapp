package adapters

import (
	"context"
	"errors"
	"fmt"
	"io"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	commonv1 "github.com/whatsapp-v2/server/internal/proto/gen/whatsapp/common/v1"
	rpcv1 "github.com/whatsapp-v2/server/internal/proto/gen/whatsapp/rpc/v1"
	wsv1 "github.com/whatsapp-v2/server/internal/proto/gen/whatsapp/ws/v1"
)

// GRPCChatClient implements gateway.ChatClient over the rpc.v1.ChatService
// (ws-gateway-lld §2: 2 s timeout — set by the caller's context — and one
// retry on transport unavailability).
type GRPCChatClient struct{ c rpcv1.ChatServiceClient }

func NewGRPCChatClient(conn grpc.ClientConnInterface) *GRPCChatClient {
	return &GRPCChatClient{c: rpcv1.NewChatServiceClient(conn)}
}

// AcceptMessage forwards the send. Unavailable is retried once (idempotent
// by msg_uuid — a duplicate accept returns the identical ack).
func (g *GRPCChatClient) AcceptMessage(ctx context.Context, senderUserID, senderDeviceID string, msg *wsv1.MsgSend) (*wsv1.MsgAck, *commonv1.Error, error) {
	req := &rpcv1.AcceptMessageRequest{
		SenderUserId:   senderUserID,
		SenderDeviceId: senderDeviceID,
		Msg:            msg,
	}
	resp, err := g.c.AcceptMessage(ctx, req)
	if status.Code(err) == codes.Unavailable {
		resp, err = g.c.AcceptMessage(ctx, req)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("accept rpc: %w", err)
	}
	return resp.GetAck(), resp.GetError(), nil
}

// PullInbox drains the replay stream, emitting each non-empty batch.
func (g *GRPCChatClient) PullInbox(ctx context.Context, deviceID string, cursors []*wsv1.ConversationCursor, emit func(items []*wsv1.InboxItem) error) error {
	stream, err := g.c.PullInbox(ctx, &rpcv1.PullInboxRequest{
		RecipientDeviceId: deviceID,
		Cursors:           cursors,
	})
	if err != nil {
		return fmt.Errorf("pull inbox rpc: %w", err)
	}
	for {
		resp, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return nil // server closed after end_of_replay
		}
		if err != nil {
			return fmt.Errorf("pull inbox stream: %w", err)
		}
		if items := resp.GetItems(); len(items) > 0 {
			if err := emit(items); err != nil {
				return err
			}
		}
		if resp.GetEndOfReplay() {
			return nil
		}
	}
}

// AckDelivered forwards the cumulative watermark. A typed rejection is
// surfaced as an error: the caller only logs it (acks are retried by design
// — unacked rows simply replay).
func (g *GRPCChatClient) AckDelivered(ctx context.Context, deviceID string, upTo []*wsv1.ConversationCursor) error {
	resp, err := g.c.AckDelivered(ctx, &rpcv1.AckDeliveredRequest{
		RecipientDeviceId: deviceID,
		UpTo:              upTo,
	})
	if err != nil {
		return fmt.Errorf("ack rpc: %w", err)
	}
	if werr := resp.GetError(); werr != nil {
		return fmt.Errorf("ack rejected: %s", werr.GetCode())
	}
	return nil
}
