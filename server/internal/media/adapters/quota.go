package adapters

import (
	"context"

	"google.golang.org/grpc"

	"github.com/whatsapp-v2/server/internal/media"
	rpcv1 "github.com/whatsapp-v2/server/internal/proto/gen/whatsapp/rpc/v1"
)

// QuotaClient calls core-api's QuotaService — the single-writer storage counter
// (media-svc-lld §3). A gRPC error propagates so the service fails closed.
type QuotaClient struct{ client rpcv1.QuotaServiceClient }

func NewQuotaClient(conn grpc.ClientConnInterface) *QuotaClient {
	return &QuotaClient{client: rpcv1.NewQuotaServiceClient(conn)}
}

func (q *QuotaClient) CheckAndReserve(ctx context.Context, userID string, bytes int64) (bool, int64, error) {
	resp, err := q.client.CheckAndReserve(ctx, &rpcv1.CheckAndReserveRequest{
		UserId:         userID,
		BytesRequested: bytes,
	})
	if err != nil {
		return false, 0, err
	}
	return resp.GetAllowed(), resp.GetRemainingBytes(), nil
}

var _ media.Quota = (*QuotaClient)(nil)
