package adapters

import (
	"context"
	"errors"
	"log/slog"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/whatsapp-v2/server/internal/chat"
	"github.com/whatsapp-v2/server/internal/platform/httpx"
	commonv1 "github.com/whatsapp-v2/server/internal/proto/gen/whatsapp/common/v1"
	rpcv1 "github.com/whatsapp-v2/server/internal/proto/gen/whatsapp/rpc/v1"
	wsv1 "github.com/whatsapp-v2/server/internal/proto/gen/whatsapp/ws/v1"
)

// ChatGRPC exposes the chat service to ws-gateway as rpc.v1.ChatService.
// Domain rejections travel as a typed common.v1.Error in the response;
// gRPC status codes are reserved for transport-level failures
// (rpc.proto, design-patterns §2).
type ChatGRPC struct {
	rpcv1.UnimplementedChatServiceServer
	svc *chat.Service
	log *slog.Logger
}

func NewChatGRPC(svc *chat.Service, log *slog.Logger) *ChatGRPC {
	return &ChatGRPC{svc: svc, log: log}
}

// AcceptMessage runs the accept hot path. The sender identity was
// authenticated by the gateway at Hello — never trusted from the frame.
func (g *ChatGRPC) AcceptMessage(ctx context.Context, req *rpcv1.AcceptMessageRequest) (*rpcv1.AcceptMessageResponse, error) {
	msg := req.GetMsg()
	if msg == nil {
		return nil, status.Error(codes.InvalidArgument, "msg is required")
	}
	res, err := g.svc.Accept(ctx, chat.AcceptRequest{
		SenderUserID:   req.GetSenderUserId(),
		SenderDeviceID: req.GetSenderDeviceId(),
		ConversationID: msg.GetConversationId(),
		MsgUUID:        msg.GetMsgUuid(),
		Kind:           chat.MsgKind(msg.GetKind()),
		OverlayTarget:  msg.GetOverlayTarget(),
		Ciphertext:     msg.GetSealedEnvelope(),
	})
	if err != nil {
		if werr := wireError(err); werr != nil {
			return &rpcv1.AcceptMessageResponse{Error: werr}, nil
		}
		g.log.Error("accept failed", "err", err)
		return nil, status.Error(codes.Internal, "accept failed")
	}
	return &rpcv1.AcceptMessageResponse{Ack: &wsv1.MsgAck{
		MsgUuid:        msg.GetMsgUuid(),
		ConversationId: msg.GetConversationId(),
		Seq:            res.Seq,
		ServerTimeMs:   res.ServerTimeMS,
	}}, nil
}

// PullInbox streams the device's pending inbox in batches, ending with an
// end_of_replay marker. PullInboxResponse carries no error field, so domain
// rejections map to InvalidArgument here.
func (g *ChatGRPC) PullInbox(req *rpcv1.PullInboxRequest, stream rpcv1.ChatService_PullInboxServer) error {
	cursors := make([]chat.Cursor, 0, len(req.GetCursors()))
	for _, c := range req.GetCursors() {
		cursors = append(cursors, chat.Cursor{ConversationID: c.GetConversationId(), LastSeq: c.GetLastSeq()})
	}
	err := g.svc.PullInbox(stream.Context(), req.GetRecipientDeviceId(), cursors, int(req.GetBatchSize()),
		func(batch []chat.InboxItem) error {
			items := make([]*wsv1.InboxItem, len(batch))
			for i, it := range batch {
				items[i] = wireItem(it)
			}
			return stream.Send(&rpcv1.PullInboxResponse{Items: items})
		})
	if err != nil {
		var ae *httpx.APIError
		if errors.As(err, &ae) {
			return status.Error(codes.InvalidArgument, ae.Message)
		}
		g.log.Error("inbox replay failed", "device_id", req.GetRecipientDeviceId(), "err", err)
		return status.Error(codes.Internal, "replay failed")
	}
	return stream.Send(&rpcv1.PullInboxResponse{EndOfReplay: true})
}

// AckDelivered applies the client's cumulative persistence watermark.
func (g *ChatGRPC) AckDelivered(ctx context.Context, req *rpcv1.AckDeliveredRequest) (*rpcv1.AckDeliveredResponse, error) {
	upTo := make([]chat.Cursor, 0, len(req.GetUpTo()))
	for _, c := range req.GetUpTo() {
		upTo = append(upTo, chat.Cursor{ConversationID: c.GetConversationId(), LastSeq: c.GetLastSeq()})
	}
	if err := g.svc.AckDelivered(ctx, req.GetRecipientDeviceId(), upTo); err != nil {
		if werr := wireError(err); werr != nil {
			return &rpcv1.AckDeliveredResponse{Error: werr}, nil
		}
		g.log.Error("ack delete failed", "device_id", req.GetRecipientDeviceId(), "err", err)
		return nil, status.Error(codes.Internal, "ack failed")
	}
	return &rpcv1.AckDeliveredResponse{}, nil
}

// SubmitReceipt relays a recipient's cumulative delivered/read watermark to
// the conversation's other devices (privacy-gated for read receipts).
func (g *ChatGRPC) SubmitReceipt(ctx context.Context, req *rpcv1.SubmitReceiptRequest) (*rpcv1.SubmitReceiptResponse, error) {
	rc := req.GetReceipt()
	if rc == nil {
		return nil, status.Error(codes.InvalidArgument, "receipt is required")
	}
	err := g.svc.SubmitReceipt(ctx, req.GetFromUserId(), req.GetFromDeviceId(), chat.ReceiptIn{
		ConversationID: rc.GetConversationId(),
		Kind:           chat.ReceiptKind(rc.GetKind()),
		UpToSeq:        rc.GetUpToSeq(),
	})
	if err != nil {
		if werr := wireError(err); werr != nil {
			return &rpcv1.SubmitReceiptResponse{Error: werr}, nil
		}
		g.log.Error("submit receipt failed", "err", err)
		return nil, status.Error(codes.Internal, "receipt failed")
	}
	return &rpcv1.SubmitReceiptResponse{}, nil
}

// wireError maps a domain rejection (httpx.APIError) to the uniform
// common.v1.Error envelope; nil for everything else (→ transport status).
func wireError(err error) *commonv1.Error {
	var ae *httpx.APIError
	if !errors.As(err, &ae) {
		return nil
	}
	return &commonv1.Error{
		Code:         ae.Code,
		Message:      ae.Message,
		Retryable:    ae.Retryable,
		RetryAfterMs: uint64(ae.RetryAfter.Milliseconds()), //nolint:gosec // RetryAfter is never negative
	}
}
