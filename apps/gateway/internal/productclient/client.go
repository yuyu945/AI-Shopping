package productclient

import (
	"context"

	productpb "github.com/yuyu945/AI-Shopping/services/product-service/gen"
	"google.golang.org/grpc"
)

// Client is the Gateway's read-only product dependency contract.
type Client interface {
	ListProducts(context.Context, *productpb.ListProductsRequest) (*productpb.ListProductsResponse, error)
	GetProduct(context.Context, *productpb.GetProductRequest) (*productpb.GetProductResponse, error)
}

type grpcClient struct {
	client productpb.ProductServiceClient
}

func NewGRPCClient(conn grpc.ClientConnInterface) Client {
	return &grpcClient{client: productpb.NewProductServiceClient(conn)}
}

func (c *grpcClient) ListProducts(ctx context.Context, req *productpb.ListProductsRequest) (*productpb.ListProductsResponse, error) {
	return c.client.ListProducts(ctx, req)
}

func (c *grpcClient) GetProduct(ctx context.Context, req *productpb.GetProductRequest) (*productpb.GetProductResponse, error) {
	return c.client.GetProduct(ctx, req)
}
