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

func TestOrderGRPCServerRejectsMissingBearerForWalletPayment(t *testing.T) {
	manager, err := platformauth.NewManager([]byte("01234567890123456789012345678901"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = NewGRPCServer(order.NewService(nil, nil, nil), manager, time.Second).PayWallet(context.Background(), &orderpb.PayWalletRequest{OrderNo: "ORD-1"})
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

func TestOrderGRPCServerSubmitReviewForwardsAuthenticatedRequest(t *testing.T) {
	manager, ctx := authenticatedOrderContext(t, 7)
	repository := &transportRepository{}
	server := NewGRPCServer(order.NewService(repository, nil, nil), manager, time.Second)

	response, err := server.SubmitReview(ctx, &orderpb.SubmitReviewRequest{OrderNo: "ORD-1", SkuId: 101, Rating: 5, Content: "good"})
	if err != nil {
		t.Fatalf("SubmitReview: %v", err)
	}
	if repository.lastReview.UserID != 7 || repository.lastReview.OrderNo != "ORD-1" || repository.lastReview.SKUID != 101 || repository.lastReview.Rating != 5 || repository.lastReview.Content != "good" {
		t.Fatalf("repository review = %#v, want forwarded request", repository.lastReview)
	}
	if response.GetReview().GetReviewNo() == "" || response.GetReview().GetProductId() != 21 || response.GetReview().GetStatus() != string(order.PublishedReview) {
		t.Fatalf("response = %#v, want review DTO", response.GetReview())
	}
}

func TestOrderGRPCServerSubmitReviewRequiresAuthentication(t *testing.T) {
	manager, err := platformauth.NewManager([]byte("01234567890123456789012345678901"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = NewGRPCServer(order.NewService(&transportRepository{}, nil, nil), manager, time.Second).SubmitReview(context.Background(), &orderpb.SubmitReviewRequest{OrderNo: "ORD-1", SkuId: 101, Rating: 5, Content: "good"})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("code = %v, want unauthenticated", status.Code(err))
	}
}

func TestOrderGRPCServerSubmitReviewMapsDomainErrors(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want codes.Code
	}{
		{name: "duplicate", err: &order.Error{Code: order.IdempotencyConflict, Message: "review already exists"}, want: codes.AlreadyExists},
		{name: "invalid", err: &order.Error{Code: order.InvalidArgument, Message: "rating must be between 1 and 5"}, want: codes.InvalidArgument},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			manager, ctx := authenticatedOrderContext(t, 7)
			repository := &transportRepository{submitReviewErr: tc.err}
			_, err := NewGRPCServer(order.NewService(repository, nil, nil), manager, time.Second).SubmitReview(ctx, &orderpb.SubmitReviewRequest{OrderNo: "ORD-1", SkuId: 101, Rating: 5, Content: "good"})
			if status.Code(err) != tc.want {
				t.Fatalf("code = %v, want %v", status.Code(err), tc.want)
			}
		})
	}
}

func TestOrderStatusMapsDependencyTimeout(t *testing.T) {
	if got := status.Code(orderStatus(&order.Error{Code: order.DependencyTimeout})); got != codes.DeadlineExceeded {
		t.Fatalf("status code = %v, want deadline exceeded", got)
	}
}

func TestOrderStatusMapsPaymentErrors(t *testing.T) {
	cases := map[order.Code]codes.Code{
		order.OutOfStock: codes.FailedPrecondition, order.InsufficientBalance: codes.ResourceExhausted,
		order.PaymentInProgress: codes.Aborted, order.IdempotencyConflict: codes.AlreadyExists,
	}
	for code, want := range cases {
		if got := status.Code(orderStatus(&order.Error{Code: code})); got != want {
			t.Fatalf("%s=%v, want %v", code, got, want)
		}
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

type transportRepository struct {
	lastReview      order.Review
	submitReviewErr error
}

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
func (r *transportRepository) SubmitReview(_ context.Context, review order.Review) (order.Review, error) {
	r.lastReview = review
	if r.submitReviewErr != nil {
		return order.Review{}, r.submitReviewErr
	}
	review.ProductID = 21
	review.Status = order.PublishedReview
	return review, nil
}

func authenticatedOrderContext(t *testing.T, userID uint64) (*platformauth.Manager, context.Context) {
	t.Helper()
	manager, err := platformauth.NewManager([]byte("01234567890123456789012345678901"))
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := manager.Issue(platformauth.Principal{UserID: userID, Email: "ada@example.com"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return manager, metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer "+token))
}
