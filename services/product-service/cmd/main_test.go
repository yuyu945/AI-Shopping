package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/yuyu945/AI-Shopping/services/product-service/internal/catalog"
	"github.com/zeromicro/go-zero/core/conf"
)

func TestCatalogDSNUsesCatalogDatabase(t *testing.T) {
	got, err := catalogDSN("app:secret@tcp(localhost:3306)/user_db?parseTime=true")
	if err != nil || !strings.Contains(got, "/catalog_db?") {
		t.Fatalf("dsn=%q err=%v", got, err)
	}
}

type fakeCacheClient struct {
	pingErr error
	closed  bool
	cache   catalog.DetailCache
}

func (f *fakeCacheClient) Ping(context.Context) error       { return f.pingErr }
func (f *fakeCacheClient) Close() error                     { f.closed = true; return nil }
func (f *fakeCacheClient) DetailCache() catalog.DetailCache { return f.cache }

func TestBuildDetailCacheFallsBackWhenRedisUnavailable(t *testing.T) {
	client := &fakeCacheClient{pingErr: errors.New("redis password=must-not-leak")}
	cache, closeFn, err := buildDetailCache(context.Background(), "redis:6379", time.Second, func(redisOptions) cacheClient { return client })
	if err == nil || cache != nil || closeFn != nil || !client.closed {
		t.Fatalf("cache is nil=%v close is nil=%v err=%v closed=%v", cache == nil, closeFn == nil, err, client.closed)
	}
	if strings.Contains(err.Error(), "must-not-leak") {
		t.Fatalf("unsafe error: %v", err)
	}
}

func TestBuildCacheInvalidationWorker(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var serviceConfig productServiceConfig
	if err := conf.Load("../etc/product-service.yaml", &serviceConfig); err != nil {
		t.Fatal(err)
	}
	worker, err := buildCacheInvalidationWorker(db, &fakeDetailCache{}, serviceConfig.CacheInvalidation)
	if err != nil {
		t.Fatalf("buildCacheInvalidationWorker() error = %v", err)
	}
	if worker == nil {
		t.Fatal("buildCacheInvalidationWorker() = nil, want worker")
	}
}

func TestBuildCacheInvalidationWorkerSkipsRedisDegradedStartup(t *testing.T) {
	worker, err := buildCacheInvalidationWorker(nil, nil, catalog.CacheInvalidationConfig{})
	if err != nil {
		t.Fatalf("buildCacheInvalidationWorker() error = %v", err)
	}
	if worker != nil {
		t.Fatalf("buildCacheInvalidationWorker() = %#v, want nil", worker)
	}
}

type fakeDetailCache struct{}

func (*fakeDetailCache) Get(context.Context, string) ([]byte, error) {
	return nil, catalog.ErrCacheMiss
}
func (*fakeDetailCache) Set(context.Context, string, []byte, time.Duration) error {
	return nil
}
func (*fakeDetailCache) Delete(context.Context, string) error { return nil }
