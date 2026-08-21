package server

import (
	"context"
	"strings"
	"testing"
	"time"

	productpb "github.com/yuyu945/AI-Shopping/services/product-service/gen"
	"github.com/yuyu945/AI-Shopping/services/product-service/internal/catalog"
)

type blockingRepository struct{}

func (blockingRepository) ListProducts(ctx context.Context, _ catalog.ProductFilter) ([]catalog.ProductSummary, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}
func (blockingRepository) GetProduct(ctx context.Context, _ uint64, _ *uint64) (catalog.ProductDetail, error) {
	<-ctx.Done()
	return catalog.ProductDetail{}, ctx.Err()
}

func TestGRPCServerListTimeoutMapsToDeadline(t *testing.T) {
	_, err := NewGRPCServer(catalog.NewProductService(blockingRepository{}, nil), time.Millisecond).ListProducts(context.Background(), &productpb.ListProductsRequest{Page: 1})
	if err == nil || !containsCode(err, "DeadlineExceeded") {
		t.Fatalf("expected deadline error, got %v", err)
	}
}

func TestGRPCWireMappingCopiesApplicationDTO(t *testing.T) {
	title := "Phone"
	detail := catalog.ProductDetailDTO{ProductID: 10, CategoryID: 9, Title: title, Promotions: []catalog.PromotionSummaryDTO{{PromotionID: 20, RuleType: "THRESHOLD", ThresholdAmount: stringPtr("100"), DiscountAmount: stringPtr("10")}}}
	wire := mapProductDetail(detail)
	if wire.GetProductId() != 10 || len(wire.GetPromotions()) != 1 || wire.GetPromotions()[0].GetThresholdAmount() != "100" {
		t.Fatalf("unexpected wire mapping: %#v", wire)
	}
}

func TestGRPCListMappingCopiesIndependentSummary(t *testing.T) {
	wire := mapProductSummary(catalog.ProductSummaryDTO{ProductID: 3, CategoryID: 4, Title: "List", StockQty: 2})
	if wire.GetProductId() != 3 || wire.GetCategoryId() != 4 || wire.GetStockStatus() != "IN_STOCK" {
		t.Fatalf("unexpected list wire mapping: %#v", wire)
	}
}

func containsCode(err error, want string) bool {
	return err != nil && strings.Contains(err.Error(), want)
}
func stringPtr(v string) *string { return &v }
