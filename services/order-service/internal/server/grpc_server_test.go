package server

import (
	"context"
	"testing"
	"time"

	platformauth "github.com/yuyu945/AI-Shopping/internal/platform/auth"
	orderpb "github.com/yuyu945/AI-Shopping/services/order-service/gen"
	"github.com/yuyu945/AI-Shopping/services/order-service/internal/order"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestOrderGRPCServerRejectsMissingBearer(t *testing.T) {
	manager, err := platformauth.NewManager([]byte("01234567890123456789012345678901"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = NewGRPCServer(order.NewService(nil, nil, nil), manager, time.Second).GetCart(context.Background(), &orderpb.GetCartRequest{})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("code = %v, want unauthenticated", status.Code(err))
	}
}

func TestOrderGRPCServerRejectsInvalidRequestID(t *testing.T) {
	manager, err := platformauth.NewManager([]byte("01234567890123456789012345678901"))
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := manager.Issue(platformauth.Principal{UserID: 7, Email: "ada@example.com"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer "+token))
	_, err = NewGRPCServer(order.NewService(nil, nil, nil), manager, time.Second).CreateOrder(ctx, &orderpb.CreateOrderRequest{RequestId: "", AddressId: 1})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("code = %v, want invalid argument", status.Code(err))
	}
}

func TestOrderStatusMapsDependencyTimeout(t *testing.T) {
	if got := status.Code(orderStatus(&order.Error{Code: order.DependencyTimeout})); got != codes.DeadlineExceeded {
		t.Fatalf("status code = %v, want deadline exceeded", got)
	}
}

func TestOrderGRPCServerScopesForeignResourcesAndRejectsInvalidIDs(t *testing.T) {
	manager, err := platformauth.NewManager([]byte("01234567890123456789012345678901"))
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := manager.Issue(platformauth.Principal{UserID: 7, Email: "ada@example.com"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer "+token))
	server := NewGRPCServer(order.NewService(transportRepository{}, nil, nil), manager, time.Second)
	if _, err := server.GetOrder(ctx, &orderpb.GetOrderRequest{OrderNo: "foreign"}); status.Code(err) != codes.NotFound {
		t.Fatalf("foreign order code = %v", status.Code(err))
	}
	if _, err := server.UpdateCartItem(ctx, &orderpb.UpdateCartItemRequest{CartItemId: 99, Quantity: 1}); status.Code(err) != codes.NotFound {
		t.Fatalf("foreign cart item code = %v", status.Code(err))
	}
	for name, call := range map[string]func() error{
		"sku":       func() error { _, err := server.AddCartItem(ctx, &orderpb.AddCartItemRequest{Quantity: 1}); return err },
		"cart item": func() error { _, err := server.DeleteCartItem(ctx, &orderpb.DeleteCartItemRequest{}); return err },
		"address": func() error {
			_, err := server.CreateOrder(ctx, &orderpb.CreateOrderRequest{RequestId: "request"})
			return err
		},
		"request id":   func() error { _, err := server.CreateOrder(ctx, &orderpb.CreateOrderRequest{AddressId: 1}); return err },
		"order number": func() error { _, err := server.GetOrder(ctx, &orderpb.GetOrderRequest{}); return err },
	} {
		t.Run(name, func(t *testing.T) {
			if got := status.Code(call()); got != codes.InvalidArgument {
				t.Fatalf("code = %v", got)
			}
		})
	}
}

type transportRepository struct{}

func (transportRepository) GetCart(context.Context, uint64) (order.Cart, error) {
	return order.Cart{}, nil
}
func (transportRepository) AddCartItem(context.Context, uint64, order.AddCartItemInput) (order.CartItem, error) {
	return order.CartItem{}, nil
}
func (transportRepository) UpdateCartItem(context.Context, uint64, uint64, order.UpdateCartItemInput) error {
	return order.ErrNotFound
}
func (transportRepository) DeleteCartItem(context.Context, uint64, uint64) error {
	return order.ErrNotFound
}
func (transportRepository) FindOrderByRequest(context.Context, uint64, string) (order.Order, error) {
	return order.Order{}, order.ErrNotFound
}
func (transportRepository) GetOrder(context.Context, uint64, string) (order.Order, error) {
	return order.Order{}, order.ErrNotFound
}
func (transportRepository) ListOrders(context.Context, uint64) ([]order.Order, error) {
	return nil, nil
}
func (transportRepository) CreateOrder(context.Context, order.Order) (order.Order, error) {
	return order.Order{}, nil
}
