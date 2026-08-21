package server

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/yuyu945/AI-Shopping/internal/platform/apperror"
	productpb "github.com/yuyu945/AI-Shopping/services/product-service/gen"
	"github.com/yuyu945/AI-Shopping/services/product-service/internal/catalog"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// GRPCServer exposes read-only catalog APIs over the generated product contract.
type GRPCServer struct {
	productpb.UnimplementedProductServiceServer
	service *catalog.ProductService
	timeout time.Duration
}

func NewGRPCServer(service *catalog.ProductService, timeout time.Duration) *GRPCServer {
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	return &GRPCServer{service: service, timeout: timeout}
}

func (s *GRPCServer) ListProducts(ctx context.Context, req *productpb.ListProductsRequest) (*productpb.ListProductsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	page := int(req.GetPage())
	if page == 0 {
		page = 1
	}
	items, err := s.withTimeoutList(ctx, func(callCtx context.Context) ([]catalog.ProductSummaryDTO, error) {
		return s.service.ListProducts(callCtx, catalog.ProductFilter{
			Keyword: req.GetKeyword(), CategoryID: req.GetCategoryId(), Page: page, PageSize: int(req.GetPageSize()),
		})
	})
	if err != nil {
		return nil, toStatusError(err)
	}
	result := &productpb.ListProductsResponse{Products: make([]*productpb.ProductSummary, 0, len(items))}
	for _, item := range items {
		result.Products = append(result.Products, mapProductSummary(item))
	}
	return result, nil
}

func (s *GRPCServer) GetProduct(ctx context.Context, req *productpb.GetProductRequest) (*productpb.GetProductResponse, error) {
	if req == nil || req.GetProductId() == 0 {
		return nil, status.Error(codes.InvalidArgument, "product_id is required")
	}
	var skuID *uint64
	if req.SkuId != nil {
		value := req.GetSkuId()
		skuID = &value
	}
	detail, err := s.withTimeout(ctx, func(callCtx context.Context) (catalog.ProductDetailDTO, error) {
		return s.service.GetProduct(callCtx, req.GetProductId(), skuID)
	})
	if err != nil {
		return nil, toStatusError(err)
	}
	return &productpb.GetProductResponse{Product: mapProductDetail(detail)}, nil
}

func (s *GRPCServer) withTimeout(ctx context.Context, fn func(context.Context) (catalog.ProductDetailDTO, error)) (catalog.ProductDetailDTO, error) {
	callCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	return fn(callCtx)
}

func (s *GRPCServer) withTimeoutList(ctx context.Context, fn func(context.Context) ([]catalog.ProductSummaryDTO, error)) ([]catalog.ProductSummaryDTO, error) {
	callCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	return fn(callCtx)
}

func mapProductSummary(item catalog.ProductSummaryDTO) *productpb.ProductSummary {
	result := &productpb.ProductSummary{ProductId: item.ID, CategoryId: item.CategoryID, Title: item.Title, StockQty: item.StockQty, StockStatus: stockStatus(item.StockQty)}
	if item.Subtitle != nil {
		result.Subtitle = item.Subtitle
	}
	if item.MinPrice != nil {
		result.MinSalePrice = item.MinPrice
	}
	return result
}

func mapProductDetail(item catalog.ProductDetailDTO) *productpb.Product {
	result := &productpb.Product{ProductId: item.ID, CategoryId: item.CategoryID, Title: item.Title}
	if item.Subtitle != nil {
		result.Subtitle = item.Subtitle
	}
	if item.DetailMarkdown != nil {
		result.DetailMarkdown = item.DetailMarkdown
	}
	for _, sku := range item.SKUs {
		result.Skus = append(result.Skus, mapSKU(sku))
	}
	for _, image := range item.Images {
		result.Images = append(result.Images, &productpb.ImageRef{ImageId: image.ID, ObjectKey: image.ObjectKey, SortNo: image.SortNo})
	}
	for _, promotion := range item.Promotions {
		mapped := &productpb.PromotionSummary{PromotionId: promotion.ID, RuleType: promotion.RuleType, DiscountAmount: promotion.DiscountAmount}
		if promotion.ThresholdAmount != nil {
			mapped.ThresholdAmount = promotion.ThresholdAmount
		}
		result.Promotions = append(result.Promotions, mapped)
	}
	return result
}

func mapSKU(item catalog.SKUDetail) *productpb.Sku {
	result := &productpb.Sku{SkuId: item.ID, SkuCode: item.SKUCode, SalePrice: item.SalePrice, StockQty: item.Inventory.AvailableQty, StockStatus: stockStatus(item.Inventory.AvailableQty), Specs: map[string]string{}}
	var specs map[string]string
	if json.Unmarshal(item.SpecJSON, &specs) == nil {
		result.Specs = specs
	}
	return result
}

func stockStatus(qty uint64) string {
	if qty > 0 {
		return "IN_STOCK"
	}
	return "OUT_OF_STOCK"
}

func toStatusError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return status.Error(codes.DeadlineExceeded, "dependency timeout")
	}
	var appErr *apperror.Error
	if !errors.As(err, &appErr) {
		return status.Error(codes.Internal, "internal server error")
	}
	switch appErr.Code {
	case apperror.InvalidArgument:
		return status.Error(codes.InvalidArgument, appErr.Message)
	case apperror.NotFound:
		return status.Error(codes.NotFound, appErr.Message)
	case apperror.DependencyTimeout:
		return status.Error(codes.DeadlineExceeded, "dependency timeout")
	default:
		return status.Error(codes.Internal, "internal server error")
	}
}
