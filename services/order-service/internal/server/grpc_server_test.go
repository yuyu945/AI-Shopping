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
