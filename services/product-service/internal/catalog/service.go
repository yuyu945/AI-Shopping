package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/yuyu945/AI-Shopping/internal/platform/apperror"
)

// ProductRepository is the persistence contract consumed by ProductService.
type ProductRepository interface {
	ListProducts(context.Context, ProductFilter) ([]ProductSummary, error)
	GetProduct(context.Context, uint64, *uint64) (ProductDetail, error)
}

// DetailCache stores serialized product details for a bounded period.
type DetailCache interface {
	Get(context.Context, string) ([]byte, error)
	Set(context.Context, string, []byte, time.Duration) error
	Delete(context.Context, string) error
}

// ProductSummaryDTO and ProductDetailDTO identify service response DTOs while
// retaining the catalog's existing read models as the stable wire shape.
type ProductSummaryDTO = ProductSummary
type ProductDetailDTO = ProductDetail

// ProductService validates catalog reads and applies cache-aside detail reads.
type ProductService struct {
	repository ProductRepository
	cache      DetailCache
}

// NewProductService constructs a product application service.
func NewProductService(repository ProductRepository, cache DetailCache) *ProductService {
	return &ProductService{repository: repository, cache: cache}
}

// ListProducts validates and normalizes a product filter before querying the repository.
func (s *ProductService) ListProducts(ctx context.Context, filter ProductFilter) ([]ProductSummaryDTO, error) {
	normalized, err := normalizeProductFilter(filter)
	if err != nil {
		return nil, err
	}
	rows, err := s.repository.ListProducts(ctx, normalized)
	if err != nil {
		return nil, safeDependencyError("list products", err)
	}
	return mapProductSummaries(rows), nil
}

// GetProduct reads a detail from cache first and falls back to the repository.
func (s *ProductService) GetProduct(ctx context.Context, productID uint64, skuID *uint64) (ProductDetailDTO, error) {
	// Freeze the optional identifier before any cache or repository call. The
	// caller may reuse and mutate its pointer while an external dependency runs.
	requestedSKU := cloneUint64Ptr(skuID)
	key := ProductCacheKey(productID, requestedSKU)
	if s.cache != nil {
		if payload, err := s.cache.Get(ctx, key); err == nil {
			var cached ProductDetail
			if json.Unmarshal(payload, &cached) == nil && validCachedDetail(cached, productID, requestedSKU) {
				return cloneProductDetail(cached), nil
			}
			// A malformed value must not poison subsequent reads.
			_ = s.cache.Delete(ctx, key)
		}
	}

	detail, err := s.repository.GetProduct(ctx, productID, requestedSKU)
	if err != nil {
		return ProductDetailDTO{}, safeProductError(err)
	}
	result := cloneProductDetail(detail)
	if s.cache != nil {
		if payload, marshalErr := json.Marshal(result); marshalErr == nil {
			_ = s.cache.Set(ctx, key, payload, 60*time.Second)
		}
	}
	return result, nil
}

func validCachedDetail(detail ProductDetail, productID uint64, skuID *uint64) bool {
	if detail.ID == 0 || detail.ID != productID {
		return false
	}
	if skuID == nil {
		return true
	}
	for _, sku := range detail.SKUs {
		if sku.ID == *skuID {
			return true
		}
	}
	return false
}

func normalizeProductFilter(filter ProductFilter) (ProductFilter, error) {
	if filter.Page < 1 {
		return ProductFilter{}, apperror.New(apperror.InvalidArgument, "page must be at least 1")
	}
	if filter.PageSize < 0 || filter.PageSize > 100 {
		return ProductFilter{}, apperror.New(apperror.InvalidArgument, "page_size must be between 1 and 100")
	}
	if filter.PageSize == 0 {
		filter.PageSize = 20
	}
	filter.Keyword = strings.TrimSpace(filter.Keyword)
	return filter, nil
}

func mapProductSummaries(rows []ProductSummary) []ProductSummaryDTO {
	result := make([]ProductSummaryDTO, len(rows))
	copy(result, rows)
	for i := range result {
		result[i].BrandID = cloneUint64Ptr(rows[i].BrandID)
		result[i].Subtitle = cloneStringPtr(rows[i].Subtitle)
		result[i].MinPrice = cloneStringPtr(rows[i].MinPrice)
	}
	return result
}

func cloneProductDetail(detail ProductDetail) ProductDetail {
	result := detail
	result.BrandID = cloneUint64Ptr(detail.BrandID)
	result.Subtitle = cloneStringPtr(detail.Subtitle)
	result.MinPrice = cloneStringPtr(detail.MinPrice)
	result.DetailMarkdown = cloneStringPtr(detail.DetailMarkdown)
	result.SKUs = append([]SKUDetail(nil), detail.SKUs...)
	for i := range result.SKUs {
		result.SKUs[i].SpecJSON = append([]byte(nil), detail.SKUs[i].SpecJSON...)
	}
	result.Images = append([]ImageRef(nil), detail.Images...)
	return result
}

func cloneUint64Ptr(value *uint64) *uint64 {
	if value == nil {
		return nil
	}
	result := *value
	return &result
}

func cloneStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	result := *value
	return &result
}

func safeProductError(err error) error {
	var notFound *NotFoundError
	if errors.As(err, &notFound) {
		return apperror.Wrap(apperror.NotFound, "product not found", err)
	}
	return safeDependencyError("get product", err)
}

func safeDependencyError(operation string, err error) error {
	return apperror.Wrap(apperror.Internal, operation+" failed", err)
}
