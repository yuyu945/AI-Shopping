package userclient

import (
	"context"

	platformauth "github.com/yuyu945/AI-Shopping/internal/platform/auth"
	userpb "github.com/yuyu945/AI-Shopping/services/user-service/gen"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

type Client interface {
	Register(context.Context, *userpb.RegisterRequest) (*userpb.AuthResponse, error)
	Login(context.Context, *userpb.LoginRequest) (*userpb.AuthResponse, error)
	GetMyProfile(context.Context, *userpb.GetMyProfileRequest) (*userpb.GetMyProfileResponse, error)
	UpdateMyProfile(context.Context, *userpb.UpdateMyProfileRequest) (*userpb.GetMyProfileResponse, error)
	ListMyAddresses(context.Context, *userpb.ListMyAddressesRequest) (*userpb.ListMyAddressesResponse, error)
	CreateMyAddress(context.Context, *userpb.CreateMyAddressRequest) (*userpb.AddressResponse, error)
	UpdateMyAddress(context.Context, *userpb.UpdateMyAddressRequest) (*userpb.AddressResponse, error)
	DeleteMyAddress(context.Context, *userpb.DeleteMyAddressRequest) (*userpb.Empty, error)
}
type grpcClient struct{ client userpb.UserServiceClient }

func NewGRPCClient(conn grpc.ClientConnInterface) Client {
	return &grpcClient{userpb.NewUserServiceClient(conn)}
}
func (c *grpcClient) auth(ctx context.Context) context.Context {
	if b, ok := platformauth.BearerFromContext(ctx); ok {
		return metadata.AppendToOutgoingContext(ctx, "authorization", b)
	}
	return ctx
}
func (c *grpcClient) Register(ctx context.Context, r *userpb.RegisterRequest) (*userpb.AuthResponse, error) {
	return c.client.Register(ctx, r)
}
func (c *grpcClient) Login(ctx context.Context, r *userpb.LoginRequest) (*userpb.AuthResponse, error) {
	return c.client.Login(ctx, r)
}
func (c *grpcClient) GetMyProfile(ctx context.Context, r *userpb.GetMyProfileRequest) (*userpb.GetMyProfileResponse, error) {
	return c.client.GetMyProfile(c.auth(ctx), r)
}
func (c *grpcClient) UpdateMyProfile(ctx context.Context, r *userpb.UpdateMyProfileRequest) (*userpb.GetMyProfileResponse, error) {
	return c.client.UpdateMyProfile(c.auth(ctx), r)
}
func (c *grpcClient) ListMyAddresses(ctx context.Context, r *userpb.ListMyAddressesRequest) (*userpb.ListMyAddressesResponse, error) {
	return c.client.ListMyAddresses(c.auth(ctx), r)
}
func (c *grpcClient) CreateMyAddress(ctx context.Context, r *userpb.CreateMyAddressRequest) (*userpb.AddressResponse, error) {
	return c.client.CreateMyAddress(c.auth(ctx), r)
}
func (c *grpcClient) UpdateMyAddress(ctx context.Context, r *userpb.UpdateMyAddressRequest) (*userpb.AddressResponse, error) {
	return c.client.UpdateMyAddress(c.auth(ctx), r)
}
func (c *grpcClient) DeleteMyAddress(ctx context.Context, r *userpb.DeleteMyAddressRequest) (*userpb.Empty, error) {
	return c.client.DeleteMyAddress(c.auth(ctx), r)
}
