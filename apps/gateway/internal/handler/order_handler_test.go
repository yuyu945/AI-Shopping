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
	add     func(context.Context, *orderpb.AddCartItemRequest) (*orderpb.CartItemResponse, error)
	update  func(context.Context, *orderpb.UpdateCartItemRequest) (*orderpb.Empty, error)
	delete  func(context.Context, *orderpb.DeleteCartItemRequest) (*orderpb.Empty, error)
	list    func(context.Context, *orderpb.ListOrdersRequest) (*orderpb.ListOrdersResponse, error)
	get     func(context.Context, *orderpb.GetOrderRequest) (*orderpb.OrderResponse, error)
}

func (f *fakeOrderClient) GetCart(c context.Context, r *orderpb.GetCartRequest) (*orderpb.GetCartResponse, error) {
	return f.getCart(c, r)
}
func (f *fakeOrderClient) AddCartItem(ctx context.Context, req *orderpb.AddCartItemRequest) (*orderpb.CartItemResponse, error) {
	if f.add != nil {
		return f.add(ctx, req)
	}
	return &orderpb.CartItemResponse{Item: &orderpb.CartItem{}}, nil
}
func (f *fakeOrderClient) UpdateCartItem(ctx context.Context, req *orderpb.UpdateCartItemRequest) (*orderpb.Empty, error) {
	if f.update != nil {
		return f.update(ctx, req)
	}
	return &orderpb.Empty{}, nil
}
func (f *fakeOrderClient) DeleteCartItem(ctx context.Context, req *orderpb.DeleteCartItemRequest) (*orderpb.Empty, error) {
	if f.delete != nil {
		return f.delete(ctx, req)
	}
	return &orderpb.Empty{}, nil
}
func (f *fakeOrderClient) CreateOrder(c context.Context, r *orderpb.CreateOrderRequest) (*orderpb.OrderResponse, error) {
	return f.create(c, r)
}
func (f *fakeOrderClient) ListOrders(ctx context.Context, req *orderpb.ListOrdersRequest) (*orderpb.ListOrdersResponse, error) {
	if f.list != nil {
		return f.list(ctx, req)
	}
	return &orderpb.ListOrdersResponse{}, nil
}
func (f *fakeOrderClient) GetOrder(ctx context.Context, req *orderpb.GetOrderRequest) (*orderpb.OrderResponse, error) {
	if f.get != nil {
		return f.get(ctx, req)
	}
	return &orderpb.OrderResponse{Order: &orderpb.Order{}}, nil
}

func TestOrderHandlerServesAllRegisteredCartAndOrderMethods(t *testing.T) {
	h := NewOrderHandler(&fakeOrderClient{getCart: func(context.Context, *orderpb.GetCartRequest) (*orderpb.GetCartResponse, error) {
		return &orderpb.GetCartResponse{Cart: &orderpb.Cart{}}, nil
	}, create: func(context.Context, *orderpb.CreateOrderRequest) (*orderpb.OrderResponse, error) {
		return &orderpb.OrderResponse{Order: &orderpb.Order{}}, nil
	}})
	cases := []struct {
		name, method, path, body string
		handler                  http.HandlerFunc
	}{
		{"get cart", http.MethodGet, "/api/v1/cart", "", h.Cart()},
		{"add cart item", http.MethodPost, "/api/v1/cart/items", `{"sku_id":1,"quantity":1}`, h.Cart()},
		{"update cart item", http.MethodPut, "/api/v1/cart/items/1", `{"quantity":1}`, h.CartItem()},
		{"delete cart item", http.MethodDelete, "/api/v1/cart/items/1", "", h.CartItem()},
		{"list orders", http.MethodGet, "/api/v1/orders", "", h.Orders()},
		{"create order", http.MethodPost, "/api/v1/orders", `{"request_id":"request","address_id":1}`, h.Orders()},
		{"get order", http.MethodGet, "/api/v1/orders/ORD-1", "", h.Order()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			if strings.Contains(tc.path, "/items/") {
				r.SetPathValue("id", "1")
			}
			if strings.Contains(tc.path, "/orders/") {
				r.SetPathValue("order_no", "ORD-1")
			}
			w := httptest.NewRecorder()
			tc.handler.ServeHTTP(w, r)
			if w.Code != http.StatusOK && w.Code != http.StatusNoContent {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
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

func TestOrderHandlerMapsDependencyTimeoutWithoutDetails(t *testing.T) {
	h := NewOrderHandler(&fakeOrderClient{getCart: func(context.Context, *orderpb.GetCartRequest) (*orderpb.GetCartResponse, error) {
		return nil, status.Error(codes.DeadlineExceeded, "database password must not leak")
	}})
	w := httptest.NewRecorder()
	h.Cart().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/cart", nil))
	if w.Code != http.StatusGatewayTimeout || !strings.Contains(w.Body.String(), `"code":"DEPENDENCY_TIMEOUT"`) || strings.Contains(w.Body.String(), "password") {
		t.Fatalf("response=%d %s", w.Code, w.Body.String())
	}
}
