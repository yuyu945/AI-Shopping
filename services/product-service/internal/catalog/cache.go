package catalog

import (
	"errors"
	"fmt"
)

// ErrCacheMiss indicates that a detail is not present in the cache.
var ErrCacheMiss = errors.New("cache miss")

const productDetailCacheVersion = "v1"

// ProductCacheKey returns the versioned key used for one product/SKU detail.
func ProductCacheKey(productID uint64, skuID *uint64) string {
	sku := "all"
	if skuID != nil {
		sku = fmt.Sprintf("%d", *skuID)
	}
	return fmt.Sprintf("product:%s:detail:%d:sku:%s", productDetailCacheVersion, productID, sku)
}
