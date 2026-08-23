package client

import (
	"context"
	"time"

	orderpb "github.com/yuyu945/AI-Shopping/services/order-service/gen"
	"github.com/yuyu945/AI-Shopping/services/product-service/internal/catalog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// SettlementClient invokes order-service's internal, read-only payment status RPC.
type SettlementClient struct {
	client  orderpb.OrderServiceClient
	token   string
	timeout time.Duration
}

func NewSettlementClient(conn grpc.ClientConnInterface, token string, timeout time.Duration) *SettlementClient {
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	return &SettlementClient{client: orderpb.NewOrderServiceClient(conn), token: token, timeout: timeout}
}
func (c *SettlementClient) GetPaymentSettlementStatus(ctx context.Context, orderNo, paymentAttemptID string) (catalog.SettlementState, error) {
	call := metadata.AppendToOutgoingContext(ctx, "x-ai-shopping-service-token", c.token)
	call, cancel := context.WithTimeout(call, c.timeout)
	defer cancel()
	out, err := c.client.GetPaymentSettlementStatus(call, &orderpb.GetPaymentSettlementStatusRequest{OrderNo: orderNo, PaymentAttemptId: paymentAttemptID})
	if err != nil {
		return "", err
	}
	return catalog.SettlementState(out.GetStatus()), nil
}
