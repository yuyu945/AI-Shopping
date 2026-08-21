package catalog

import (
	"context"
	"errors"
	"fmt"
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
	service := newTestCatalogMutationService(t, store, cache, func() time.Time { return now }, delay, timeout)

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
	for i, deadline := range cache.deleteDeadlines {
		remaining := time.Until(deadline)
		if remaining > timeout || remaining < timeout-200*time.Millisecond {
			t.Fatalf("delete %d remaining timeout = %s, want close to %s", i, remaining, timeout)
		}
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
		newContext     func() (context.Context, context.CancelFunc)
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
		{
			name:           "store deadline exceeded",
			productID:      10,
			storeErr:       fmt.Errorf("mysql password=highly-sensitive-value: %w", context.DeadlineExceeded),
			wantCode:       apperror.DependencyTimeout,
			wantStoreCalls: 1,
			forbiddenText:  "highly-sensitive-value",
		},
		{
			name:      "request deadline exceeded",
			productID: 10,
			storeErr:  errors.New("mysql dsn=root:secret@tcp(db)/catalog_db unavailable"),
			newContext: func() (context.Context, context.CancelFunc) {
				return context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
			},
			wantCode:       apperror.DependencyTimeout,
			wantStoreCalls: 1,
			forbiddenText:  "root:secret",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &fakeProductMutationStore{err: tt.storeErr}
			cache := &fakeMutationDetailCache{}
			service := newTestCatalogMutationService(t, store, cache, time.Now, time.Minute, time.Second)
			ctx := context.Background()
			cancel := func() {}
			if tt.newContext != nil {
				ctx, cancel = tt.newContext()
			}
			defer cancel()

			_, err := service.UpdateProductDetail(ctx, tt.productID, "updated detail")
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

func TestNewCatalogMutationServiceRejectsNonPositiveCacheCallTimeout(t *testing.T) {
	for _, timeout := range []time.Duration{0, -time.Second} {
		t.Run(timeout.String(), func(t *testing.T) {
			service, err := NewCatalogMutationService(&fakeProductMutationStore{}, &fakeMutationDetailCache{}, time.Now, time.Minute, timeout)
			assertAppErrorCode(t, err, apperror.InvalidArgument)
			if service != nil {
				t.Fatalf("NewCatalogMutationService() = %#v, want nil", service)
			}
		})
	}
}

func TestCatalogMutationServiceUpdateProductDetailAllowsNilCache(t *testing.T) {
	result := MutationResult{ProductID: 10, DetailMarkdown: "updated detail", CacheKeys: []string{"key-one"}}
	service := newTestCatalogMutationService(t, &fakeProductMutationStore{result: result}, nil, time.Now, time.Minute, time.Second)

	got, err := service.UpdateProductDetail(context.Background(), 10, "updated detail")
	if err != nil {
		t.Fatalf("UpdateProductDetail() error = %v", err)
	}
	if !reflect.DeepEqual(got, result) {
		t.Fatalf("UpdateProductDetail() = %#v, want %#v", got, result)
	}
}

func TestCatalogMutationServiceUpdateProductDetailIgnoresCacheDeleteErrors(t *testing.T) {
	result := MutationResult{ProductID: 10, DetailMarkdown: "updated detail", CacheKeys: []string{"key-one", "key-two"}}
	store := &fakeProductMutationStore{result: result}
	cache := &fakeMutationDetailCache{deleteErrors: map[string]error{"key-one": errors.New("redis password=highly-sensitive-value unavailable")}}
	service := newTestCatalogMutationService(t, store, cache, time.Now, time.Minute, time.Second)

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

func TestCatalogMutationServiceUpdateProductDetailIgnoresCacheDeadlineExceeded(t *testing.T) {
	result := MutationResult{ProductID: 10, DetailMarkdown: "updated detail", CacheKeys: []string{"key-one"}}
	store := &fakeProductMutationStore{result: result}
	cache := &fakeMutationDetailCache{waitForContextDone: true}
	service := newTestCatalogMutationService(t, store, cache, time.Now, time.Minute, 15*time.Millisecond)

	got, err := service.UpdateProductDetail(context.Background(), 10, "updated detail")
	if err != nil {
		t.Fatalf("UpdateProductDetail() error = %v", err)
	}
	if !reflect.DeepEqual(got, result) {
		t.Fatalf("UpdateProductDetail() = %#v, want %#v", got, result)
	}
	if !errors.Is(cache.deleteErrors["key-one"], context.DeadlineExceeded) {
		t.Fatalf("cache Delete() error = %v, want context deadline exceeded", cache.deleteErrors["key-one"])
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
	waitForContextDone    bool
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
	if c.waitForContextDone {
		<-ctx.Done()
		if c.deleteErrors == nil {
			c.deleteErrors = make(map[string]error)
		}
		c.deleteErrors[key] = ctx.Err()
	}
	return c.deleteErrors[key]
}

func newTestCatalogMutationService(t *testing.T, store ProductMutationStore, cache DetailCache, now func() time.Time, delayedDeleteDelay, cacheCallTimeout time.Duration) *CatalogMutationService {
	t.Helper()
	service, err := NewCatalogMutationService(store, cache, now, delayedDeleteDelay, cacheCallTimeout)
	if err != nil {
		t.Fatalf("NewCatalogMutationService() error = %v", err)
	}
	return service
}

func assertAppErrorCode(t *testing.T, err error, want apperror.Code) {
	t.Helper()
	var appErr *apperror.Error
	if !errors.As(err, &appErr) || appErr.Code != want {
		t.Fatalf("error = %v, want apperror code %s", err, want)
	}
}
