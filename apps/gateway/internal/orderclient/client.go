package orderclient

import (
	"context"
	platformauth "github.com/yuyu945/AI-Shopping/internal/platform/auth"
	platformtrace "github.com/yuyu945/AI-Shopping/internal/platform/trace"
	orderpb "github.com/yuyu945/AI-Shopping/services/order-service/gen"
	oteltrace "go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

type Client interface {
	GetCart(context.Context, *orderpb.GetCartRequest) (*orderpb.GetCartResponse, error)
	AddCartItem(context.Context, *orderpb.AddCartItemRequest) (*orderpb.CartItemResponse, error)
	UpdateCartItem(context.Context, *orderpb.UpdateCartItemRequest) (*orderpb.Empty, error)
	DeleteCartItem(context.Context, *orderpb.DeleteCartItemRequest) (*orderpb.Empty, error)
	CreateOrder(context.Context, *orderpb.CreateOrderRequest) (*orderpb.OrderResponse, error)
	ListOrders(context.Context, *orderpb.ListOrdersRequest) (*orderpb.ListOrdersResponse, error)
	GetOrder(context.Context, *orderpb.GetOrderRequest) (*orderpb.OrderResponse, error)
}
type grpcClient struct{ client orderpb.OrderServiceClient }

func NewGRPCClient(c grpc.ClientConnInterface) Client {
	return &grpcClient{orderpb.NewOrderServiceClient(c)}
}
func (c *grpcClient) auth(ctx context.Context) context.Context {
	if bearer, ok := platformauth.BearerFromContext(ctx); ok {
		ctx = metadata.AppendToOutgoingContext(ctx, "authorization", bearer)
	}
	if traceID := platformtrace.TraceID(ctx); validTraceID(traceID) {
		ctx = metadata.AppendToOutgoingContext(ctx, "trace_id", traceID)
	}
	return ctx
}

func validTraceID(value string) bool {
	_, err := oteltrace.TraceIDFromHex(value)
	return err == nil
}
func (c *grpcClient) GetCart(x context.Context, r *orderpb.GetCartRequest) (*orderpb.GetCartResponse, error) {
	return c.client.GetCart(c.auth(x), r)
}
func (c *grpcClient) AddCartItem(x context.Context, r *orderpb.AddCartItemRequest) (*orderpb.CartItemResponse, error) {
	return c.client.AddCartItem(c.auth(x), r)
}
func (c *grpcClient) UpdateCartItem(x context.Context, r *orderpb.UpdateCartItemRequest) (*orderpb.Empty, error) {
	return c.client.UpdateCartItem(c.auth(x), r)
}
func (c *grpcClient) DeleteCartItem(x context.Context, r *orderpb.DeleteCartItemRequest) (*orderpb.Empty, error) {
	return c.client.DeleteCartItem(c.auth(x), r)
}
func (c *grpcClient) CreateOrder(x context.Context, r *orderpb.CreateOrderRequest) (*orderpb.OrderResponse, error) {
	return c.client.CreateOrder(c.auth(x), r)
}
func (c *grpcClient) ListOrders(x context.Context, r *orderpb.ListOrdersRequest) (*orderpb.ListOrdersResponse, error) {
	return c.client.ListOrders(c.auth(x), r)
}
func (c *grpcClient) GetOrder(x context.Context, r *orderpb.GetOrderRequest) (*orderpb.OrderResponse, error) {
	return c.client.GetOrder(c.auth(x), r)
}
