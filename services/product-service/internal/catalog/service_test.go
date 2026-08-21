package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/yuyu945/AI-Shopping/internal/platform/apperror"
)

type fakeProductRepository struct {
	listFn func(context.Context, ProductFilter) ([]ProductSummary, error)
	getFn  func(context.Context, uint64, *uint64) (ProductDetail, error)
	listN  int
	getN   int
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
	values map[string][]byte
	gets   []string
	sets   []cacheSet
	dels   []string
	getErr error
}

type cacheSet struct {
	key   string
	value []byte
	ttl   time.Duration
}

func newFakeDetailCache() *fakeDetailCache { return &fakeDetailCache{values: map[string][]byte{}} }
func (f *fakeDetailCache) Get(_ context.Context, key string) ([]byte, error) {
	f.gets = append(f.gets, key)
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
	if err != nil || len(got) != 1 || got[0].ID != 10 {
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
	if err != nil || got.ID != 10 || repo.getN != 1 {
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
	if err != nil || got.Title != "cached" {
		t.Fatalf("unexpected cached result: %#v, %v", got, err)
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
