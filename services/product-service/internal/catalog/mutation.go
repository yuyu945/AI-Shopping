package catalog

import (
	"context"
	"errors"
	"time"

	"github.com/yuyu945/AI-Shopping/internal/platform/apperror"
)

// ProductMutationStore persists a product detail update and its cache invalidation tasks atomically.
type ProductMutationStore interface {
	UpdateProductDetailAndCreateTasks(context.Context, uint64, string, time.Time) (MutationResult, error)
}

// CatalogMutationService coordinates committed catalog mutations with best-effort cache invalidation.
type CatalogMutationService struct {
	store              ProductMutationStore
	cache              DetailCache
	now                func() time.Time
	delayedDeleteDelay time.Duration
	cacheCallTimeout   time.Duration
}

// NewCatalogMutationService constructs a catalog mutation service with explicit timing dependencies.
func NewCatalogMutationService(store ProductMutationStore, cache DetailCache, now func() time.Time, delayedDeleteDelay, cacheCallTimeout time.Duration) *CatalogMutationService {
	if now == nil {
		now = time.Now
	}
	return &CatalogMutationService{
		store:              store,
		cache:              cache,
		now:                now,
		delayedDeleteDelay: delayedDeleteDelay,
		cacheCallTimeout:   cacheCallTimeout,
	}
}

// UpdateProductDetail persists a product detail update and immediately invalidates its committed cache keys.
func (s *CatalogMutationService) UpdateProductDetail(ctx context.Context, productID uint64, detailMarkdown string) (MutationResult, error) {
	if productID == 0 {
		return MutationResult{}, apperror.New(apperror.InvalidArgument, "product_id is required")
	}

	result, err := s.store.UpdateProductDetailAndCreateTasks(ctx, productID, detailMarkdown, s.now().Add(s.delayedDeleteDelay))
	if err != nil {
		return MutationResult{}, safeMutationError(err)
	}
	if s.cache == nil {
		return result, nil
	}

	for _, cacheKey := range result.CacheKeys {
		deleteCtx, cancel := context.WithTimeout(ctx, s.cacheCallTimeout)
		_ = s.cache.Delete(deleteCtx, cacheKey)
		cancel()
	}
	return result, nil
}

func safeMutationError(err error) error {
	var notFound *NotFoundError
	if errors.As(err, &notFound) {
		return apperror.Wrap(apperror.NotFound, "product not found", err)
	}
	return safeDependencyError("update product detail", err)
}
