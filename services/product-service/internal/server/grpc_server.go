package server

import (
	"context"
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

type productSummaryWire struct {
	ProductID, CategoryID, StockQty uint64
	Title, StockStatus              string
	Subtitle, MinSalePrice          *string
}
type productWire struct {
	ProductID, CategoryID    uint64
	Title                    string
	Subtitle, DetailMarkdown *string
	SKUs                     []skuWire
	Images                   []imageWire
	Promotions               []promotionWire
}
type skuWire struct {
	SKUID                           uint64
	SKUCode, SalePrice, StockStatus string
	Specs                           map[string]string
	StockQty                        uint64
}
type imageWire struct {
	ImageID, SortNo uint64
	ObjectKey       string
}
type promotionWire struct {
	PromotionID     uint64
	RuleType        string
	ThresholdAmount *string
	DiscountAmount  *string
}

func mapProductSummary(item catalog.ProductSummaryDTO) *productpb.ProductSummary {
	wire := productSummaryWire{ProductID: item.ProductID, CategoryID: item.CategoryID, StockQty: item.StockQty, Title: item.Title, StockStatus: stockStatus(item.StockQty), Subtitle: item.Subtitle, MinSalePrice: item.MinSalePrice}
	result := &productpb.ProductSummary{ProductId: wire.ProductID, CategoryId: wire.CategoryID, Title: wire.Title, StockQty: wire.StockQty, StockStatus: wire.StockStatus}
	if wire.Subtitle != nil {
		value := *wire.Subtitle
		result.Subtitle = &value
	}
	if wire.MinSalePrice != nil {
		value := *wire.MinSalePrice
		result.MinSalePrice = &value
	}
	return result
}

func mapProductDetail(item catalog.ProductDetailDTO) *productpb.Product {
	wire := productWire{ProductID: item.ProductID, CategoryID: item.CategoryID, Title: item.Title, Subtitle: item.Subtitle, DetailMarkdown: item.DetailMarkdown}
	for _, sku := range item.SKUs {
		wire.SKUs = append(wire.SKUs, mapSKUWire(sku))
	}
	for _, image := range item.Images {
		wire.Images = append(wire.Images, imageWire{ImageID: image.ImageID, ObjectKey: image.ObjectKey, SortNo: image.SortNo})
	}
	for _, p := range item.Promotions {
		wire.Promotions = append(wire.Promotions, promotionWire{PromotionID: p.PromotionID, RuleType: p.RuleType, ThresholdAmount: p.ThresholdAmount, DiscountAmount: p.DiscountAmount})
	}
	result := &productpb.Product{ProductId: wire.ProductID, CategoryId: wire.CategoryID, Title: wire.Title}
	if wire.Subtitle != nil {
		value := *wire.Subtitle
		result.Subtitle = &value
	}
	if wire.DetailMarkdown != nil {
		value := *wire.DetailMarkdown
		result.DetailMarkdown = &value
	}
	for _, sku := range wire.SKUs {
		result.Skus = append(result.Skus, &productpb.Sku{SkuId: sku.SKUID, SkuCode: sku.SKUCode, SalePrice: sku.SalePrice, StockQty: sku.StockQty, StockStatus: sku.StockStatus, Specs: sku.Specs})
	}
	for _, image := range wire.Images {
		result.Images = append(result.Images, &productpb.ImageRef{ImageId: image.ImageID, ObjectKey: image.ObjectKey, SortNo: image.SortNo})
	}
	for _, promotion := range wire.Promotions {
		mapped := &productpb.PromotionSummary{PromotionId: promotion.PromotionID, RuleType: promotion.RuleType}
		if promotion.ThresholdAmount != nil {
			value := *promotion.ThresholdAmount
			mapped.ThresholdAmount = &value
		}
		if promotion.DiscountAmount != nil {
			value := *promotion.DiscountAmount
			mapped.DiscountAmount = &value
		}
		result.Promotions = append(result.Promotions, mapped)
	}
	return result
}

func mapSKUWire(item catalog.SKUDetailDTO) skuWire {
	return skuWire{SKUID: item.SKUID, SKUCode: item.SKUCode, SalePrice: item.SalePrice, StockQty: item.StockQty, StockStatus: stockStatus(item.StockQty), Specs: item.Specs}
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
