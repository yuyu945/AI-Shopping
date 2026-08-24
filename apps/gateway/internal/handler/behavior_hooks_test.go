package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yuyu945/AI-Shopping/apps/gateway/internal/behavior"
	orderpb "github.com/yuyu945/AI-Shopping/services/order-service/gen"
	productpb "github.com/yuyu945/AI-Shopping/services/product-service/gen"
)

func TestProductDetailRecordsBehaviorEventAfterSuccess(t *testing.T) {
	recorder := &fakeBehaviorRecorder{}
	h := NewProductHandlerWithBehavior(&fakeProductClient{getFn: func(context.Context, *productpb.GetProductRequest) (*productpb.GetProductResponse, error) {
		return &productpb.GetProductResponse{Product: &productpb.Product{ProductId: 1001, Title: "Laptop"}}, nil
	}}, recorder)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/products/1001", nil)
	r.SetPathValue("id", "1001")

	h.Get().ServeHTTP(w, r)

	if w.Code != http.StatusOK || recorder.last.EventType != "product.viewed" || recorder.last.ResourceID != "1001" {
		t.Fatalf("status=%d event=%#v body=%s", w.Code, recorder.last, w.Body.String())
	}
}

func TestAddCartItemRecordsBehaviorEventAfterSuccess(t *testing.T) {
	recorder := &fakeBehaviorRecorder{}
	h := NewOrderHandlerWithBehavior(&fakeOrderClient{add: func(context.Context, *orderpb.AddCartItemRequest) (*orderpb.CartItemResponse, error) {
		return &orderpb.CartItemResponse{Item: &orderpb.CartItem{CartItemId: 9, SkuId: 2001, Quantity: 1, Selected: true}}, nil
	}}, recorder)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/cart/items", strings.NewReader(`{"sku_id":2001,"quantity":1,"selected":true}`))

	h.Cart().ServeHTTP(w, r)

	if w.Code != http.StatusOK || recorder.last.EventType != "cart.item_added" || recorder.last.ResourceID != "2001" {
		t.Fatalf("status=%d event=%#v body=%s", w.Code, recorder.last, w.Body.String())
	}
}

type fakeBehaviorRecorder struct {
	last behavior.Event
}

func (f *fakeBehaviorRecorder) Record(_ context.Context, event behavior.Event) error {
	f.last = event
	return nil
}
