package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	orderpb "github.com/yuyu945/AI-Shopping/services/order-service/gen"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type fakeOrderClient struct {
	getCart func(context.Context, *orderpb.GetCartRequest) (*orderpb.GetCartResponse, error)
	create  func(context.Context, *orderpb.CreateOrderRequest) (*orderpb.OrderResponse, error)
}

func (f *fakeOrderClient) GetCart(c context.Context, r *orderpb.GetCartRequest) (*orderpb.GetCartResponse, error) {
	return f.getCart(c, r)
}
func (f *fakeOrderClient) AddCartItem(context.Context, *orderpb.AddCartItemRequest) (*orderpb.CartItemResponse, error) {
	return nil, nil
}
func (f *fakeOrderClient) UpdateCartItem(context.Context, *orderpb.UpdateCartItemRequest) (*orderpb.Empty, error) {
	return nil, nil
}
func (f *fakeOrderClient) DeleteCartItem(context.Context, *orderpb.DeleteCartItemRequest) (*orderpb.Empty, error) {
	return nil, nil
}
func (f *fakeOrderClient) CreateOrder(c context.Context, r *orderpb.CreateOrderRequest) (*orderpb.OrderResponse, error) {
	return f.create(c, r)
}
func (f *fakeOrderClient) ListOrders(context.Context, *orderpb.ListOrdersRequest) (*orderpb.ListOrdersResponse, error) {
	return nil, nil
}
func (f *fakeOrderClient) GetOrder(context.Context, *orderpb.GetOrderRequest) (*orderpb.OrderResponse, error) {
	return nil, nil
}

func TestOrderHandlerCartAndCreateOrder(t *testing.T) {
	c := &fakeOrderClient{getCart: func(context.Context, *orderpb.GetCartRequest) (*orderpb.GetCartResponse, error) {
		return &orderpb.GetCartResponse{Cart: &orderpb.Cart{Items: []*orderpb.CartItem{{CartItemId: 2, SkuId: 3, Quantity: 1, Selected: true}}}}, nil
	}, create: func(_ context.Context, r *orderpb.CreateOrderRequest) (*orderpb.OrderResponse, error) {
		if r.GetRequestId() != "req-1" || r.GetAddressId() != 8 {
			t.Fatalf("request=%#v", r)
		}
		return &orderpb.OrderResponse{Order: &orderpb.Order{OrderNo: "ORD-1", Status: "PENDING_PAYMENT"}}, nil
	}}
	h := NewOrderHandler(c)
	w := httptest.NewRecorder()
	h.Cart().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/cart", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"sku_id":3`) {
		t.Fatalf("cart=%d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	h.Orders().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/orders", strings.NewReader(`{"request_id":"req-1","address_id":8}`)))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"order_no":"ORD-1"`) {
		t.Fatalf("order=%d %s", w.Code, w.Body.String())
	}
}
func TestOrderHandlerMapsConflictWithoutDetails(t *testing.T) {
	h := NewOrderHandler(&fakeOrderClient{getCart: func(context.Context, *orderpb.GetCartRequest) (*orderpb.GetCartResponse, error) {
		return nil, status.Error(codes.AlreadyExists, "secret db")
	}})
	w := httptest.NewRecorder()
	h.Cart().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/cart", nil))
	if w.Code != http.StatusConflict || strings.Contains(w.Body.String(), "secret") {
		t.Fatalf("response=%d %s", w.Code, w.Body.String())
	}
}
