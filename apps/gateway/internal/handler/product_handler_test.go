package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	productpb "github.com/yuyu945/AI-Shopping/services/product-service/gen"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type fakeProductClient struct {
	listFn func(context.Context, *productpb.ListProductsRequest) (*productpb.ListProductsResponse, error)
	getFn  func(context.Context, *productpb.GetProductRequest) (*productpb.GetProductResponse, error)
}

func (f *fakeProductClient) ListProducts(ctx context.Context, req *productpb.ListProductsRequest) (*productpb.ListProductsResponse, error) {
	return f.listFn(ctx, req)
}

func (f *fakeProductClient) GetProduct(ctx context.Context, req *productpb.GetProductRequest) (*productpb.GetProductResponse, error) {
	return f.getFn(ctx, req)
}

func TestProductHandlerListMapsQueryAndResponse(t *testing.T) {
	client := &fakeProductClient{listFn: func(_ context.Context, req *productpb.ListProductsRequest) (*productpb.ListProductsResponse, error) {
		if req.GetKeyword() != "phone" || req.GetCategoryId() != 9 || req.GetPage() != 2 || req.GetPageSize() != 10 {
			t.Fatalf("unexpected request: %#v", req)
		}
		return &productpb.ListProductsResponse{Products: []*productpb.ProductSummary{{ProductId: 10, Title: "Phone"}}}, nil
	}}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/products?keyword=phone&category_id=9&page=2&page_size=10", nil)
	request.Header.Set("X-Trace-ID", "4bf92f3577b34da6a3ce929d0e0e4736")

	NewProductHandler(client).List().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"product_id":10`) {
		t.Fatalf("unexpected response: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("X-Trace-ID"); got != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Errorf("X-Trace-ID = %q", got)
	}
}

func TestProductHandlerGetMapsSKUAndNotFound(t *testing.T) {
	client := &fakeProductClient{getFn: func(_ context.Context, req *productpb.GetProductRequest) (*productpb.GetProductResponse, error) {
		if req.GetProductId() != 10 || req.GetSkuId() != 7 {
			t.Fatalf("unexpected request: %#v", req)
		}
		return nil, status.Error(codes.NotFound, "sql details must not leak")
	}}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/products/10?sku_id=7", nil)

	NewProductHandler(client).Get().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), "sql details") || !strings.Contains(recorder.Body.String(), `"code":"NOT_FOUND"`) {
		t.Fatalf("unsafe error body: %s", recorder.Body.String())
	}
}

func TestProductHandlerMapsDependencyTimeout(t *testing.T) {
	client := &fakeProductClient{listFn: func(context.Context, *productpb.ListProductsRequest) (*productpb.ListProductsResponse, error) {
		return nil, errors.New("dial tcp 10.0.0.1: secret")
	}}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/products", nil)

	NewProductHandler(client).List().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusInternalServerError || !strings.Contains(recorder.Body.String(), `"code":"INTERNAL"`) {
		t.Fatalf("unexpected response: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), "10.0.0.1") {
		t.Fatalf("dependency error leaked: %s", recorder.Body.String())
	}
}
