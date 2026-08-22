// Package server exposes authenticated order operations over gRPC.
package server

import (
	"context"
	"errors"
	"time"

	platformauth "github.com/yuyu945/AI-Shopping/internal/platform/auth"
	orderpb "github.com/yuyu945/AI-Shopping/services/order-service/gen"
	"github.com/yuyu945/AI-Shopping/services/order-service/internal/order"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type GRPCServer struct {
	orderpb.UnimplementedOrderServiceServer
	service *order.Service
	payment *order.PaymentService
	auth    *platformauth.Manager
	timeout time.Duration
}

// NewGRPCServerWithPayment exposes cart/order operations and wallet payment.
func NewGRPCServerWithPayment(service *order.Service, payment *order.PaymentService, auth *platformauth.Manager, timeout time.Duration) *GRPCServer {
	server := NewGRPCServer(service, auth, timeout)
	server.payment = payment
	return server
}

func NewGRPCServer(service *order.Service, auth *platformauth.Manager, timeout time.Duration) *GRPCServer {
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	return &GRPCServer{service: service, auth: auth, timeout: timeout}
}
func (s *GRPCServer) userID(ctx context.Context) (uint64, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok || len(md.Get("authorization")) != 1 {
		return 0, status.Error(codes.Unauthenticated, "authentication required")
	}
	p, err := s.auth.VerifyBearer(md.Get("authorization")[0], time.Now())
	if err != nil {
		return 0, status.Error(codes.Unauthenticated, "authentication required")
	}
	return p.UserID, nil
}
func (s *GRPCServer) call(ctx context.Context, fn func(context.Context, uint64) error) error {
	id, e := s.userID(ctx)
	if e != nil {
		return e
	}
	c, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	if e = fn(c, id); e != nil {
		if errors.Is(c.Err(), context.DeadlineExceeded) {
			return status.Error(codes.DeadlineExceeded, "dependency timeout")
		}
		return orderStatus(e)
	}
	return nil
}
func (s *GRPCServer) GetCart(ctx context.Context, _ *orderpb.GetCartRequest) (*orderpb.GetCartResponse, error) {
	var out order.Cart
	err := s.call(ctx, func(c context.Context, id uint64) error { var e error; out, e = s.service.GetCart(c, id); return e })
	if err != nil {
		return nil, err
	}
	return &orderpb.GetCartResponse{Cart: cartWire(out)}, nil
}
func (s *GRPCServer) AddCartItem(ctx context.Context, r *orderpb.AddCartItemRequest) (*orderpb.CartItemResponse, error) {
	if r == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	var out order.CartItem
	err := s.call(ctx, func(c context.Context, id uint64) error {
		var e error
		out, e = s.service.AddCartItem(c, id, order.AddCartItemInput{SKUID: r.GetSkuId(), Quantity: r.GetQuantity(), Selected: r.GetSelected()})
		return e
	})
	if err != nil {
		return nil, err
	}
	return &orderpb.CartItemResponse{Item: cartItemWire(out)}, nil
}
func (s *GRPCServer) UpdateCartItem(ctx context.Context, r *orderpb.UpdateCartItemRequest) (*orderpb.Empty, error) {
	if r == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	err := s.call(ctx, func(c context.Context, id uint64) error {
		return s.service.UpdateCartItem(c, id, r.GetCartItemId(), order.UpdateCartItemInput{Quantity: r.GetQuantity(), Selected: r.GetSelected()})
	})
	if err != nil {
		return nil, err
	}
	return &orderpb.Empty{}, nil
}
func (s *GRPCServer) DeleteCartItem(ctx context.Context, r *orderpb.DeleteCartItemRequest) (*orderpb.Empty, error) {
	if r == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	err := s.call(ctx, func(c context.Context, id uint64) error { return s.service.DeleteCartItem(c, id, r.GetCartItemId()) })
	if err != nil {
		return nil, err
	}
	return &orderpb.Empty{}, nil
}
func (s *GRPCServer) CreateOrder(ctx context.Context, r *orderpb.CreateOrderRequest) (*orderpb.OrderResponse, error) {
	if r == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	var out order.Order
	err := s.call(ctx, func(c context.Context, id uint64) error {
		var e error
		out, e = s.service.CreateOrder(c, id, order.CreateOrderInput{RequestID: r.GetRequestId(), AddressID: r.GetAddressId()})
		return e
	})
	if err != nil {
		return nil, err
	}
	return &orderpb.OrderResponse{Order: orderWire(out)}, nil
}
func (s *GRPCServer) PayWallet(ctx context.Context, r *orderpb.PayWalletRequest) (*orderpb.OrderResponse, error) {
	if r == nil || r.GetOrderNo() == "" {
		return nil, status.Error(codes.InvalidArgument, "order number is required")
	}
	if _, err := s.userID(ctx); err != nil {
		return nil, err
	}
	if s.payment == nil {
		return nil, status.Error(codes.Internal, "internal server error")
	}
	var out order.Order
	err := s.call(ctx, func(c context.Context, id uint64) error {
		var e error
		out, e = s.payment.PayWallet(c, id, r.GetOrderNo())
		return e
	})
	if err != nil {
		return nil, err
	}
	return &orderpb.OrderResponse{Order: orderWire(out)}, nil
}
func (s *GRPCServer) ListOrders(ctx context.Context, _ *orderpb.ListOrdersRequest) (*orderpb.ListOrdersResponse, error) {
	var out []order.Order
	err := s.call(ctx, func(c context.Context, id uint64) error { var e error; out, e = s.service.ListOrders(c, id); return e })
	if err != nil {
		return nil, err
	}
	result := &orderpb.ListOrdersResponse{}
	for _, v := range out {
		result.Orders = append(result.Orders, orderWire(v))
	}
	return result, nil
}
func (s *GRPCServer) GetOrder(ctx context.Context, r *orderpb.GetOrderRequest) (*orderpb.OrderResponse, error) {
	if r == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	var out order.Order
	err := s.call(ctx, func(c context.Context, id uint64) error {
		var e error
		out, e = s.service.GetOrder(c, id, r.GetOrderNo())
		return e
	})
	if err != nil {
		return nil, err
	}
	return &orderpb.OrderResponse{Order: orderWire(out)}, nil
}
func orderStatus(err error) error {
	var e *order.Error
	if !errors.As(err, &e) {
		return status.Error(codes.Internal, "internal server error")
	}
	switch e.Code {
	case order.InvalidArgument:
		return status.Error(codes.InvalidArgument, e.Message)
	case order.NotFound:
		return status.Error(codes.NotFound, "resource not found")
	case order.DependencyTimeout:
		return status.Error(codes.DeadlineExceeded, "dependency timeout")
	case order.OutOfStock:
		return status.Error(codes.FailedPrecondition, "requested inventory is unavailable")
	case order.InsufficientBalance:
		return status.Error(codes.ResourceExhausted, "wallet balance is insufficient")
	case order.PaymentInProgress:
		return status.Error(codes.Aborted, "payment is in progress")
	case order.IdempotencyConflict:
		return status.Error(codes.AlreadyExists, "request conflict")
	default:
		return status.Error(codes.Internal, "internal server error")
	}
}
func cartWire(v order.Cart) *orderpb.Cart {
	out := &orderpb.Cart{}
	for _, i := range v.Items {
		out.Items = append(out.Items, cartItemWire(i))
	}
	return out
}
func cartItemWire(v order.CartItem) *orderpb.CartItem {
	return &orderpb.CartItem{CartItemId: v.ID, SkuId: v.SKUID, Quantity: v.Quantity, Selected: v.Selected}
}
func orderWire(v order.Order) *orderpb.Order {
	out := &orderpb.Order{OrderNo: v.OrderNo, RequestId: v.RequestID, Status: string(v.Status), TotalAmount: v.TotalAmount, PaidAmount: v.PaidAmount, ShippingAddress: &orderpb.ShippingAddressSnapshot{ReceiverName: v.Shipping.ReceiverName, ReceiverPhone: v.Shipping.ReceiverPhone, Province: v.Shipping.Province, City: v.Shipping.City, District: v.Shipping.District, Detail: v.Shipping.Detail}}
	for _, i := range v.Items {
		item := &orderpb.OrderItem{ProductId: i.ProductID, SkuId: i.SKUID, ProductTitle: i.ProductTitleSnapshot, SkuCode: i.SKUCodeSnapshot, SkuSpecJson: append([]byte(nil), i.SpecSnapshot...), UnitPrice: i.UnitPrice, DiscountAmount: i.DiscountAmount, Quantity: i.Quantity, ItemAmount: i.ItemAmount}
		for _, p := range i.CandidatePromotions {
			item.CandidatePromotions = append(item.CandidatePromotions, promotionWire(p))
		}
		if i.AppliedPromotion != nil {
			item.AppliedPromotion = promotionWire(*i.AppliedPromotion)
		}
		out.Items = append(out.Items, item)
	}
	return out
}
func promotionWire(v order.PromotionSnapshot) *orderpb.PromotionSnapshot {
	out := &orderpb.PromotionSnapshot{PromotionId: v.PromotionID, RuleType: v.RuleType}
	if v.ThresholdAmount != "" {
		x := v.ThresholdAmount
		out.ThresholdAmount = &x
	}
	if v.DiscountAmount != "" {
		x := v.DiscountAmount
		out.DiscountAmount = &x
	}
	return out
}
