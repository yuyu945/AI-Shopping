package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/yuyu945/AI-Shopping/internal/platform/apperror"
)

type fakeProductRepository struct {
	listFn     func(context.Context, ProductFilter) ([]ProductSummary, error)
	getFn      func(context.Context, uint64, *uint64) (ProductDetail, error)
	checkoutFn func(context.Context, []uint64) ([]CheckoutSKU, error)
	listN      int
	getN       int
}

func (f *fakeProductRepository) CheckoutSKUs(ctx context.Context, skuIDs []uint64) ([]CheckoutSKU, error) {
	if f.checkoutFn == nil {
		return nil, errors.New("checkout not configured")
	}
	return f.checkoutFn(ctx, skuIDs)
}

func (f *fakeProductRepository) ListProducts(ctx context.Context, filter ProductFilter) ([]ProductSummary, error) {
	f.listN++
	return f.listFn(ctx, filter)
}

func (f *fakeProductRepository) GetProduct(ctx context.Context, id uint64, skuID *uint64) (ProductDetail, error) {
	f.getN++
	return f.getFn(ctx, id, skuID)
}

type fakeDetailCache struct {
	values    map[string][]byte
	gets      []string
	sets      []cacheSet
	dels      []string
	getErr    error
	beforeGet func()
}

type cacheSet struct {
	key   string
	value []byte
	ttl   time.Duration
}

func newFakeDetailCache() *fakeDetailCache { return &fakeDetailCache{values: map[string][]byte{}} }
func (f *fakeDetailCache) Get(_ context.Context, key string) ([]byte, error) {
	f.gets = append(f.gets, key)
	if f.beforeGet != nil {
		f.beforeGet()
	}
	if f.getErr != nil {
		return nil, f.getErr
	}
	v, ok := f.values[key]
	if !ok {
		return nil, ErrCacheMiss
	}
	return v, nil
}
func (f *fakeDetailCache) Set(_ context.Context, key string, value []byte, ttl time.Duration) error {
	f.sets = append(f.sets, cacheSet{key: key, value: value, ttl: ttl})
	f.values[key] = value
	return nil
}
func (f *fakeDetailCache) Delete(_ context.Context, key string) error {
	f.dels = append(f.dels, key)
	delete(f.values, key)
	return nil
}

func TestProductServiceListProductsNormalizesValidFilterAndMapsRows(t *testing.T) {
	repo := &fakeProductRepository{listFn: func(_ context.Context, filter ProductFilter) ([]ProductSummary, error) {
		if filter.Keyword != "phone" || filter.Page != 2 || filter.PageSize != 20 || filter.CategoryID != 9 {
			t.Fatalf("unexpected normalized filter: %#v", filter)
		}
		return []ProductSummary{{ID: 10, Title: "Phone"}}, nil
	}}
	svc := NewProductService(repo, nil)
	got, err := svc.ListProducts(context.Background(), ProductFilter{Keyword: "  phone ", CategoryID: 9, Page: 2})
	if err != nil || len(got) != 1 || got[0].ProductID != 10 {
		t.Fatalf("unexpected result: %#v, %v", got, err)
	}
}

func TestProductServiceListProductsRejectsInvalidPagination(t *testing.T) {
	repo := &fakeProductRepository{listFn: func(context.Context, ProductFilter) ([]ProductSummary, error) { return nil, nil }}
	svc := NewProductService(repo, nil)
	for _, filter := range []ProductFilter{{Page: 0}, {Page: -1}, {PageSize: -1}, {PageSize: 101}} {
		_, err := svc.ListProducts(context.Background(), filter)
		var appErr *apperror.Error
		if !errors.As(err, &appErr) || appErr.Code != apperror.InvalidArgument {
			t.Errorf("filter %#v error = %v", filter, err)
		}
	}
}

func TestProductServiceGetProductCacheMissWritesWithTTL(t *testing.T) {
	cache := newFakeDetailCache()
	repo := &fakeProductRepository{getFn: func(_ context.Context, id uint64, skuID *uint64) (ProductDetail, error) {
		if id != 10 || skuID == nil || *skuID != 7 {
			t.Fatalf("unexpected repository args: %d, %v", id, skuID)
		}
		return ProductDetail{ProductSummary: ProductSummary{ID: id}, SKUs: []SKUDetail{{ID: 7}}}, nil
	}}
	svc := NewProductService(repo, cache)
	skuID := uint64(7)
	got, err := svc.GetProduct(context.Background(), 10, &skuID)
	if err != nil || got.ProductID != 10 || repo.getN != 1 {
		t.Fatalf("unexpected result: %#v, %v", got, err)
	}
	if len(cache.sets) != 1 || cache.sets[0].ttl != 60*time.Second || !strings.Contains(cache.sets[0].key, "product:v1:detail:10:sku:7") {
		t.Fatalf("unexpected cache set: %#v", cache.sets)
	}
}

func TestProductServiceGetProductCacheHitSkipsRepository(t *testing.T) {
	cache := newFakeDetailCache()
	detail := ProductDetail{ProductSummary: ProductSummary{ID: 10, Title: "cached"}}
	payload, _ := json.Marshal(detail)
	cache.values[ProductCacheKey(10, nil)] = payload
	repo := &fakeProductRepository{getFn: func(context.Context, uint64, *uint64) (ProductDetail, error) {
		t.Fatal("repository should not be called")
		return ProductDetail{}, nil
	}}
	got, err := NewProductService(repo, cache).GetProduct(context.Background(), 10, nil)
	if err != nil || got.Title != "cached" || got.ProductID != 10 {
		t.Fatalf("unexpected cached result: %#v, %v", got, err)
	}
}

func TestProductServiceGetProductFreezesSKUIdentityBeforeCacheRead(t *testing.T) {
	requestedSKU := uint64(7)
	cache := newFakeDetailCache()
	cache.beforeGet = func() { requestedSKU = 8 }
	repo := &fakeProductRepository{getFn: func(_ context.Context, id uint64, skuID *uint64) (ProductDetail, error) {
		if id != 10 || skuID == nil || *skuID != 7 {
			t.Fatalf("repository received mutable SKU: %d, %v", id, skuID)
		}
		return ProductDetail{ProductSummary: ProductSummary{ID: id}, SKUs: []SKUDetail{{ID: 7}}}, nil
	}}
	_, err := NewProductService(repo, cache).GetProduct(context.Background(), 10, &requestedSKU)
	if err != nil || len(cache.gets) != 1 || cache.gets[0] != ProductCacheKey(10, func() *uint64 { v := uint64(7); return &v }()) {
		t.Fatalf("SKU identity was not frozen: gets=%v err=%v", cache.gets, err)
	}
}

func TestProductServiceRejectsInvalidCachedDetailIdentity(t *testing.T) {
	tests := []struct {
		name    string
		payload ProductDetail
		skuID   *uint64
	}{
		{name: "null", payload: ProductDetail{}, skuID: nil},
		{name: "wrong product", payload: ProductDetail{ProductSummary: ProductSummary{ID: 11}}, skuID: nil},
	}
	requestedSKU := uint64(7)
	tests = append(tests, struct {
		name    string
		payload ProductDetail
		skuID   *uint64
	}{name: "missing requested sku", payload: ProductDetail{ProductSummary: ProductSummary{ID: 10}, SKUs: []SKUDetail{{ID: 8}}}, skuID: &requestedSKU})

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache := newFakeDetailCache()
			key := ProductCacheKey(10, tt.skuID)
			if tt.name == "null" {
				cache.values[key] = []byte("null")
			} else {
				cache.values[key], _ = json.Marshal(tt.payload)
			}
			repo := &fakeProductRepository{getFn: func(_ context.Context, id uint64, skuID *uint64) (ProductDetail, error) {
				result := ProductDetail{ProductSummary: ProductSummary{ID: id}}
				if skuID != nil {
					result.SKUs = []SKUDetail{{ID: *skuID}}
				}
				return result, nil
			}}
			got, err := NewProductService(repo, cache).GetProduct(context.Background(), 10, tt.skuID)
			if err != nil || got.ProductID != 10 || len(cache.dels) != 1 || repo.getN != 1 {
				t.Fatalf("invalid cache was accepted: got=%#v err=%v deletes=%v repo=%d", got, err, cache.dels, repo.getN)
			}
		})
	}
}

func TestProductServiceMalformedCacheDeletesAndFallsBack(t *testing.T) {
	cache := newFakeDetailCache()
	cache.values[ProductCacheKey(10, nil)] = []byte("not-json")
	repo := &fakeProductRepository{getFn: func(context.Context, uint64, *uint64) (ProductDetail, error) {
		return ProductDetail{ProductSummary: ProductSummary{ID: 10, Title: "db"}}, nil
	}}
	got, err := NewProductService(repo, cache).GetProduct(context.Background(), 10, nil)
	if err != nil || got.Title != "db" || len(cache.dels) != 1 {
		t.Fatalf("unexpected fallback: %#v, %v, deletes=%v", got, err, cache.dels)
	}
}

func TestProductServiceRepositoryFailureDoesNotWriteCache(t *testing.T) {
	cache := newFakeDetailCache()
	repo := &fakeProductRepository{getFn: func(context.Context, uint64, *uint64) (ProductDetail, error) {
		return ProductDetail{}, errors.New("sql password=secret")
	}}
	_, err := NewProductService(repo, cache).GetProduct(context.Background(), 10, nil)
	if len(cache.sets) != 0 {
		t.Fatalf("repository failure wrote cache: %#v", cache.sets)
	}
	if strings.Contains(err.Error(), "password=secret") {
		t.Fatalf("error leaked repository detail: %v", err)
	}
}

func TestProductServiceCheckoutSKUsBypassesDetailCache(t *testing.T) {
	cache := newFakeDetailCache()
	repo := &fakeProductRepository{checkoutFn: func(_ context.Context, ids []uint64) ([]CheckoutSKU, error) {
		if len(ids) != 1 || ids[0] != 7 {
			t.Fatalf("sku ids = %v", ids)
		}
		return []CheckoutSKU{{ProductID: 10, SKUID: 7, ProductTitle: "Keyboard", SKUCode: "KB-1", SpecJSON: []byte(`{}`), SalePrice: "99.00", Saleable: true}}, nil
	}}
	got, err := NewProductService(repo, cache).CheckoutSKUs(context.Background(), []uint64{7})
	if err != nil || len(got) != 1 || got[0].SalePrice != "99.00" {
		t.Fatalf("CheckoutSKUs() = %#v, %v", got, err)
	}
	if len(cache.gets) != 0 || repo.getN != 0 {
		t.Fatalf("checkout used detail cache or GetProduct: cache=%v getN=%d", cache.gets, repo.getN)
	}
}

func TestProductServiceCheckoutSKUsPreservesCallerOrderAndClonesSnapshots(t *testing.T) {
	threshold := "100.00"
	repo := &fakeProductRepository{checkoutFn: func(_ context.Context, ids []uint64) ([]CheckoutSKU, error) {
		if got, want := ids, []uint64{8, 7}; !slices.Equal(got, want) {
			t.Fatalf("repository ids = %v, want %v", got, want)
		}
		return []CheckoutSKU{{SKUID: 8, SalePrice: "19.00", Saleable: true}, {SKUID: 7, SalePrice: "99.90", SpecJSON: []byte(`{}`), Saleable: true, Promotions: []PromotionSummary{{ID: 20, ThresholdAmount: &threshold}}}}, nil
	}}
	got, err := NewProductService(repo, newFakeDetailCache()).CheckoutSKUs(context.Background(), []uint64{8, 7, 8})
	if err != nil || len(got) != 3 || got[0].SKUID != 8 || got[1].SKUID != 7 || got[2].SKUID != 8 {
		t.Fatalf("CheckoutSKUs() = %#v, %v", got, err)
	}
	got[1].SpecJSON[0] = '['
	*got[1].Promotions[0].ThresholdAmount = "0.00"
	if got[2].SKUID != 8 || threshold != "100.00" {
		t.Fatalf("CheckoutSKUs() did not clone snapshot: %#v", got)
	}
}

func TestProductServiceCheckoutSKUsMapsMissingSKUToStableNotFound(t *testing.T) {
	repo := &fakeProductRepository{checkoutFn: func(context.Context, []uint64) ([]CheckoutSKU, error) {
		return nil, &NotFoundError{Resource: "sku", ID: 404}
	}}
	_, err := NewProductService(repo, nil).CheckoutSKUs(context.Background(), []uint64{404})
	var appErr *apperror.Error
	if !errors.As(err, &appErr) || appErr.Code != apperror.NotFound || appErr.Message != "checkout sku not found" {
		t.Fatalf("CheckoutSKUs() error = %v", err)
	}
}

func TestProductServiceCheckoutSKUsRejectsInactiveSKUWithStableNotFound(t *testing.T) {
	repo := &fakeProductRepository{checkoutFn: func(context.Context, []uint64) ([]CheckoutSKU, error) {
		return []CheckoutSKU{{ProductID: 10, SKUID: 7, Saleable: false}}, nil
	}}
	_, err := NewProductService(repo, nil).CheckoutSKUs(context.Background(), []uint64{7})
	var appErr *apperror.Error
	if !errors.As(err, &appErr) || appErr.Code != apperror.NotFound || appErr.Message != "checkout sku not found" {
		t.Fatalf("CheckoutSKUs() error = %v", err)
	}
}
