package catalog

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/yuyu945/AI-Shopping/internal/platform/apperror"
)

func TestCatalogMutationServiceUpdateProductDetailCreatesScheduledTasksBeforeDeletingCache(t *testing.T) {
	now := time.Date(2026, time.August, 21, 9, 30, 0, 0, time.UTC)
	result := MutationResult{
		ProductID:      10,
		DetailMarkdown: "updated detail",
		CacheKeys:      []string{"product:v1:detail:10:sku:all", "product:v1:detail:10:sku:100"},
	}
	store := &fakeProductMutationStore{result: result}
	cache := &fakeMutationDetailCache{}
	delay := 2 * time.Minute
	timeout := 3 * time.Second
	service := NewCatalogMutationService(store, cache, func() time.Time { return now }, delay, timeout)

	got, err := service.UpdateProductDetail(context.Background(), 10, "updated detail")
	if err != nil {
		t.Fatalf("UpdateProductDetail() error = %v", err)
	}
	if !reflect.DeepEqual(got, result) {
		t.Fatalf("UpdateProductDetail() = %#v, want %#v", got, result)
	}
	if store.calls != 1 || store.productID != 10 || store.detailMarkdown != "updated detail" {
		t.Fatalf("store call = %#v, want product update", store)
	}
	if want := now.Add(delay); !store.executeAt.Equal(want) {
		t.Fatalf("store executeAt = %s, want %s", store.executeAt, want)
	}
	if !sameStrings(cache.deletedKeys, result.CacheKeys) {
		t.Fatalf("deleted cache keys = %#v, want %#v", cache.deletedKeys, result.CacheKeys)
	}
	if len(cache.deleteDeadlines) != len(result.CacheKeys) {
		t.Fatalf("delete deadline count = %d, want %d", len(cache.deleteDeadlines), len(result.CacheKeys))
	}
	if cache.deleteWithoutDeadline {
		t.Fatal("cache delete did not receive a bounded context")
	}
}

func TestCatalogMutationServiceUpdateProductDetailErrorHandling(t *testing.T) {
	tests := []struct {
		name           string
		productID      uint64
		storeErr       error
		wantCode       apperror.Code
		wantStoreCalls int
		wantNotFound   bool
		forbiddenText  string
	}{
		{
			name:           "invalid product ID",
			productID:      0,
			wantCode:       apperror.InvalidArgument,
			wantStoreCalls: 0,
		},
		{
			name:           "store dependency failure",
			productID:      10,
			storeErr:       errors.New("mysql dsn=root:secret@tcp(db)/catalog_db unavailable"),
			wantCode:       apperror.Internal,
			wantStoreCalls: 1,
			forbiddenText:  "root:secret",
		},
		{
			name:           "product not found",
			productID:      404,
			storeErr:       &NotFoundError{Resource: "product", ID: 404},
			wantCode:       apperror.NotFound,
			wantStoreCalls: 1,
			wantNotFound:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &fakeProductMutationStore{err: tt.storeErr}
			cache := &fakeMutationDetailCache{}
			service := NewCatalogMutationService(store, cache, time.Now, time.Minute, time.Second)

			_, err := service.UpdateProductDetail(context.Background(), tt.productID, "updated detail")
			assertAppErrorCode(t, err, tt.wantCode)
			if store.calls != tt.wantStoreCalls {
				t.Fatalf("store calls = %d, want %d", store.calls, tt.wantStoreCalls)
			}
			if len(cache.deletedKeys) != 0 {
				t.Fatalf("deleted cache keys = %#v, want none", cache.deletedKeys)
			}
			if tt.forbiddenText != "" && strings.Contains(err.Error(), tt.forbiddenText) {
				t.Fatalf("UpdateProductDetail() leaked store error: %v", err)
			}
			if tt.wantNotFound {
				var notFound *NotFoundError
				if !errors.As(err, &notFound) || notFound.Resource != "product" || notFound.ID != tt.productID {
					t.Fatalf("UpdateProductDetail() error = %v, want product NotFoundError", err)
				}
			}
		})
	}
}

func TestCatalogMutationServiceUpdateProductDetailIgnoresCacheDeleteErrors(t *testing.T) {
	result := MutationResult{ProductID: 10, DetailMarkdown: "updated detail", CacheKeys: []string{"key-one", "key-two"}}
	store := &fakeProductMutationStore{result: result}
	cache := &fakeMutationDetailCache{deleteErrors: map[string]error{"key-one": errors.New("redis password=highly-sensitive-value unavailable")}}
	service := NewCatalogMutationService(store, cache, time.Now, time.Minute, time.Second)

	got, err := service.UpdateProductDetail(context.Background(), 10, "updated detail")
	if err != nil {
		t.Fatalf("UpdateProductDetail() error = %v", err)
	}
	if !reflect.DeepEqual(got, result) {
		t.Fatalf("UpdateProductDetail() = %#v, want %#v", got, result)
	}
	if !sameStrings(cache.deletedKeys, result.CacheKeys) {
		t.Fatalf("deleted cache keys = %#v, want %#v", cache.deletedKeys, result.CacheKeys)
	}
}

type fakeProductMutationStore struct {
	result         MutationResult
	err            error
	calls          int
	productID      uint64
	detailMarkdown string
	executeAt      time.Time
}

func (s *fakeProductMutationStore) UpdateProductDetailAndCreateTasks(_ context.Context, productID uint64, detailMarkdown string, executeAt time.Time) (MutationResult, error) {
	s.calls++
	s.productID = productID
	s.detailMarkdown = detailMarkdown
	s.executeAt = executeAt
	return s.result, s.err
}

type fakeMutationDetailCache struct {
	deletedKeys           []string
	deleteDeadlines       []time.Time
	deleteWithoutDeadline bool
	deleteErrors          map[string]error
}

func (c *fakeMutationDetailCache) Get(context.Context, string) ([]byte, error) {
	return nil, ErrCacheMiss
}

func (c *fakeMutationDetailCache) Set(context.Context, string, []byte, time.Duration) error {
	return nil
}

func (c *fakeMutationDetailCache) Delete(ctx context.Context, key string) error {
	c.deletedKeys = append(c.deletedKeys, key)
	deadline, ok := ctx.Deadline()
	if !ok {
		c.deleteWithoutDeadline = true
		return nil
	}
	c.deleteDeadlines = append(c.deleteDeadlines, deadline)
	return c.deleteErrors[key]
}

func assertAppErrorCode(t *testing.T, err error, want apperror.Code) {
	t.Helper()
	var appErr *apperror.Error
	if !errors.As(err, &appErr) || appErr.Code != want {
		t.Fatalf("error = %v, want apperror code %s", err, want)
	}
}
