package client

import (
	"context"
	"fmt"
	"github.com/yuyu945/AI-Shopping/internal/platform/apperror"
	"github.com/yuyu945/AI-Shopping/services/order-service/internal/order"
	productpb "github.com/yuyu945/AI-Shopping/services/product-service/gen"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"time"
)

type ProductClient struct {
	client  productpb.ProductServiceClient
	timeout time.Duration
}

// ReservationClient calls product-owned inventory reservation RPCs.
type ReservationClient struct {
	client  productpb.ProductServiceClient
	timeout time.Duration
	token   string
}

// NewReservationClient creates an authenticated, time-bounded reservation client.
func NewReservationClient(conn grpc.ClientConnInterface, token string, timeout time.Duration) *ReservationClient {
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	return &ReservationClient{client: productpb.NewProductServiceClient(conn), token: token, timeout: timeout}
}

func (c *ReservationClient) call(ctx context.Context) (context.Context, context.CancelFunc) {
	call := metadata.AppendToOutgoingContext(outgoing(ctx), "x-ai-shopping-service-token", c.token)
	return context.WithTimeout(call, c.timeout)
}

// ReserveStock reserves stock for a persisted payment attempt.
func (c *ReservationClient) ReserveStock(ctx context.Context, request order.ReserveRequest) (order.Reservation, error) {
	call, cancel := c.call(ctx)
	defer cancel()
	items := make([]*productpb.ReservationItem, 0, len(request.Items))
	for _, item := range request.Items {
		items = append(items, &productpb.ReservationItem{SkuId: item.SKUID, Quantity: item.Quantity})
	}
	out, err := c.client.ReserveStock(call, &productpb.ReserveStockRequest{ReservationId: request.ReservationID, OrderNo: request.OrderNo, PaymentAttemptId: request.PaymentAttemptID, ExpiresAtUnixMilli: request.ExpiresAt.UnixMilli(), Items: items})
	if err != nil {
		return order.Reservation{}, reservationError(err)
	}
	return reservationFromWire(out.GetReservation())
}

// ReleaseReservation requests best-effort stock compensation.
func (c *ReservationClient) ReleaseReservation(ctx context.Context, reservationID string) error {
	call, cancel := c.call(ctx)
	defer cancel()
	_, err := c.client.ReleaseReservation(call, &productpb.ReservationActionRequest{ReservationId: reservationID})
	return reservationError(err)
}

// GetReservation reads a product-owned reservation.
func (c *ReservationClient) GetReservation(ctx context.Context, reservationID string) (order.Reservation, error) {
	call, cancel := c.call(ctx)
	defer cancel()
	out, err := c.client.GetReservation(call, &productpb.GetReservationRequest{ReservationId: reservationID})
	if err != nil {
		return order.Reservation{}, reservationError(err)
	}
	return reservationFromWire(out.GetReservation())
}

// ConfirmReservation confirms stock after a durable paid event.
func (c *ReservationClient) ConfirmReservation(ctx context.Context, reservationID string) error {
	call, cancel := c.call(ctx)
	defer cancel()
	_, err := c.client.ConfirmReservation(call, &productpb.ReservationActionRequest{ReservationId: reservationID})
	return reservationError(err)
}

func reservationError(err error) error {
	if err == nil {
		return nil
	}
	if dependency := dependencyError(err); dependency != err {
		return dependency
	}
	if rpcStatus, ok := status.FromError(err); ok && rpcStatus.Code().String() == "FailedPrecondition" {
		return apperror.New(apperror.OutOfStock, "requested inventory is unavailable")
	}
	return fmt.Errorf("product reservation RPC failed")
}

func reservationFromWire(value *productpb.Reservation) (order.Reservation, error) {
	if value == nil {
		return order.Reservation{}, fmt.Errorf("product reservation response is empty")
	}
	result := order.Reservation{ReservationID: value.GetReservationId(), OrderNo: value.GetOrderNo(), PaymentAttemptID: value.GetPaymentAttemptId(), Status: order.ReservationStatus(value.GetStatus()), ExpiresAt: time.UnixMilli(value.GetExpiresAtUnixMilli()).UTC()}
	for _, item := range value.GetItems() {
		result.Items = append(result.Items, order.ReservationItem{SKUID: item.GetSkuId(), Quantity: item.GetQuantity()})
	}
	return result, nil
}

func NewProductClient(conn grpc.ClientConnInterface, timeout time.Duration) *ProductClient {
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	return &ProductClient{productpb.NewProductServiceClient(conn), timeout}
}
func (c *ProductClient) GetProducts(ctx context.Context, ids []uint64) ([]order.ProductSnapshot, error) {
	call, cancel := context.WithTimeout(outgoing(ctx), c.timeout)
	defer cancel()
	out, e := c.client.GetCheckoutSKUs(call, &productpb.CheckoutSKUsRequest{SkuIds: ids})
	if e != nil {
		return nil, dependencyError(e)
	}
	items := make([]order.ProductSnapshot, 0, len(out.GetSkus()))
	for _, v := range out.GetSkus() {
		item := order.ProductSnapshot{ProductID: v.GetProductId(), SKUID: v.GetSkuId(), ProductTitle: v.GetProductTitle(), SKUCode: v.GetSkuCode(), SpecJSON: append([]byte(nil), v.GetSpecJson()...), UnitPrice: v.GetSalePrice(), Saleable: v.GetSaleable()}
		for _, p := range v.GetPromotions() {
			item.Promotions = append(item.Promotions, order.PromotionSnapshot{PromotionID: p.GetPromotionId(), RuleType: p.GetRuleType(), ThresholdAmount: p.GetThresholdAmount(), DiscountAmount: p.GetDiscountAmount()})
		}
		items = append(items, item)
	}
	return items, nil
}
