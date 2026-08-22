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
func (blockingRepository) CheckoutSKUs(ctx context.Context, _ []uint64) ([]catalog.CheckoutSKU, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestGRPCServerListTimeoutMapsToDeadline(t *testing.T) {
	_, err := NewGRPCServer(catalog.NewProductService(blockingRepository{}, nil), time.Millisecond).ListProducts(context.Background(), &productpb.ListProductsRequest{Page: 1})
	if err == nil || !containsCode(err, "DeadlineExceeded") {
		t.Fatalf("expected deadline error, got %v", err)
	}
}

func TestGRPCServerCheckoutSKUsTimeoutMapsToDeadline(t *testing.T) {
	_, err := NewGRPCServer(catalog.NewProductService(blockingRepository{}, nil), time.Millisecond).CheckoutSKUs(context.Background(), &productpb.CheckoutSKUsRequest{SkuIds: []uint64{7}})
	if err == nil || !containsCode(err, "DeadlineExceeded") {
		t.Fatalf("expected deadline error, got %v", err)
	}
}

func TestGRPCServerCheckoutSKUsMapsIndependentSnapshots(t *testing.T) {
	threshold := "100.00"
	repo := checkoutRepository{items: []catalog.CheckoutSKU{{ProductID: 10, SKUID: 7, ProductTitle: "Keyboard", SKUCode: "KB-1", SpecJSON: []byte(`{}`), SalePrice: "99.90", Saleable: true, Promotions: []catalog.PromotionSummary{{ID: 20, RuleType: "THRESHOLD", ThresholdAmount: &threshold}}}}}
	got, err := NewGRPCServer(catalog.NewProductService(repo, nil), time.Second).CheckoutSKUs(context.Background(), &productpb.CheckoutSKUsRequest{SkuIds: []uint64{7}})
	if err != nil || len(got.GetSkus()) != 1 || got.GetSkus()[0].GetSalePrice() != "99.90" || got.GetSkus()[0].GetPromotions()[0].GetThresholdAmount() != "100.00" {
		t.Fatalf("CheckoutSKUs() = %#v, %v", got, err)
	}
	got.Skus[0].SpecJson[0] = '['
	*got.Skus[0].Promotions[0].ThresholdAmount = "0.00"
	if string(repo.items[0].SpecJSON) != "{}" || threshold != "100.00" {
		t.Fatalf("CheckoutSKUs() leaked mutable data: %#v", got)
	}
}

type checkoutRepository struct{ items []catalog.CheckoutSKU }

func (r checkoutRepository) ListProducts(context.Context, catalog.ProductFilter) ([]catalog.ProductSummary, error) {
	return nil, nil
}
func (r checkoutRepository) GetProduct(context.Context, uint64, *uint64) (catalog.ProductDetail, error) {
	return catalog.ProductDetail{}, nil
}
func (r checkoutRepository) CheckoutSKUs(context.Context, []uint64) ([]catalog.CheckoutSKU, error) {
	return r.items, nil
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
