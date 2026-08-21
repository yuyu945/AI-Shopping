package catalog

import "testing"

func TestProductCacheKeyIncludesVersionAndSKU(t *testing.T) {
	if got := ProductCacheKey(4, nil); got != "product:v1:detail:4:sku:all" {
		t.Fatalf("key = %q", got)
	}
	sku := uint64(9)
	if got := ProductCacheKey(4, &sku); got != "product:v1:detail:4:sku:9" {
		t.Fatalf("key = %q", got)
	}
}
