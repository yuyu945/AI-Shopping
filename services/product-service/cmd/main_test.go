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
	"github.com/zeromicro/go-zero/zrpc"
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

func TestProductServiceConfigBuildsCatalogMutationComponents(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var serviceConfig productServiceConfig
	if err := conf.Load("../etc/product-service.yaml", &serviceConfig); err != nil {
		t.Fatal(err)
	}
	if got, want := serviceConfig.CacheInvalidation.BatchSize, 50; got != want {
		t.Fatalf("BatchSize = %d, want %d", got, want)
	}
	if got, want := serviceConfig.CacheInvalidation.DelayedDeleteDelay, time.Second; got != want {
		t.Fatalf("DelayedDeleteDelay = %s, want %s", got, want)
	}
	if got, want := serviceConfig.CacheInvalidation.MaxRetries, 5; got != want {
		t.Fatalf("MaxRetries = %d, want %d", got, want)
	}
	if got, want := serviceConfig.CacheInvalidation.RetryMaxDelay, 30*time.Second; got != want {
		t.Fatalf("RetryMaxDelay = %s, want %s", got, want)
	}
	if got, want := serviceConfig.ConfirmationConsumer.CallTimeout, 2*time.Second; got != want {
		t.Fatalf("ConfirmationConsumer.CallTimeout = %s, want %s", got, want)
	}
	if got, want := serviceConfig.cacheInvalidationWorkerConfig().CallTimeout, 600*time.Millisecond; got != want {
		t.Fatalf("CallTimeout = %s, want %s", got, want)
	}
	_, worker, err := buildCatalogMutationComponents(db, &fakeDetailCache{}, serviceConfig)
	if err != nil {
		t.Fatalf("buildCatalogMutationComponents() error = %v", err)
	}
	if worker == nil {
		t.Fatal("buildCatalogMutationComponents() worker = nil, want worker")
	}
}

func TestBuildCatalogMutationComponentsSkipsRedisDegradedStartup(t *testing.T) {
	service, worker, err := buildCatalogMutationComponents(nil, nil, testProductServiceConfig())
	if err != nil {
		t.Fatalf("buildCatalogMutationComponents() error = %v", err)
	}
	if service == nil {
		t.Fatal("buildCatalogMutationComponents() service = nil, want service")
	}
	if worker != nil {
		t.Fatalf("buildCatalogMutationComponents() worker = %#v, want nil", worker)
	}
}

func TestBuildReservationServiceUsesPersistentCatalogStore(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	service, err := buildReservationService(db, &fakeDetailCache{}, testProductServiceConfig())
	if err != nil || service == nil {
		t.Fatalf("buildReservationService() = %#v, %v", service, err)
	}
}

func TestBuildCatalogMutationComponentsRejectsInvalidConfig(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	tests := []struct {
		name   string
		mutate func(*productServiceConfig)
	}{
		{name: "zero delayed delete delay", mutate: func(c *productServiceConfig) { c.CacheInvalidation.DelayedDeleteDelay = 0 }},
		{name: "negative delayed delete delay", mutate: func(c *productServiceConfig) { c.CacheInvalidation.DelayedDeleteDelay = -time.Second }},
		{name: "zero poll interval", mutate: func(c *productServiceConfig) { c.CacheInvalidation.PollInterval = 0 }},
		{name: "negative poll interval", mutate: func(c *productServiceConfig) { c.CacheInvalidation.PollInterval = -time.Second }},
		{name: "zero batch size", mutate: func(c *productServiceConfig) { c.CacheInvalidation.BatchSize = 0 }},
		{name: "negative batch size", mutate: func(c *productServiceConfig) { c.CacheInvalidation.BatchSize = -1 }},
		{name: "zero lease duration", mutate: func(c *productServiceConfig) { c.CacheInvalidation.LeaseDuration = 0 }},
		{name: "negative lease duration", mutate: func(c *productServiceConfig) { c.CacheInvalidation.LeaseDuration = -time.Second }},
		{name: "zero max retries", mutate: func(c *productServiceConfig) { c.CacheInvalidation.MaxRetries = 0 }},
		{name: "negative max retries", mutate: func(c *productServiceConfig) { c.CacheInvalidation.MaxRetries = -1 }},
		{name: "zero retry base delay", mutate: func(c *productServiceConfig) { c.CacheInvalidation.RetryBaseDelay = 0 }},
		{name: "negative retry base delay", mutate: func(c *productServiceConfig) { c.CacheInvalidation.RetryBaseDelay = -time.Second }},
		{name: "zero retry max delay", mutate: func(c *productServiceConfig) { c.CacheInvalidation.RetryMaxDelay = 0 }},
		{name: "negative retry max delay", mutate: func(c *productServiceConfig) { c.CacheInvalidation.RetryMaxDelay = -time.Second }},
		{name: "retry base exceeds max", mutate: func(c *productServiceConfig) {
			c.CacheInvalidation.RetryBaseDelay = c.CacheInvalidation.RetryMaxDelay + time.Second
		}},
		{name: "zero rpc timeout", mutate: func(c *productServiceConfig) { c.Timeout = 0 }},
		{name: "negative rpc timeout", mutate: func(c *productServiceConfig) { c.Timeout = -1 }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := testProductServiceConfig()
			tt.mutate(&config)
			service, worker, err := buildCatalogMutationComponents(db, &fakeDetailCache{}, config)
			if err == nil || service != nil || worker != nil {
				t.Fatalf("buildCatalogMutationComponents() = %#v, %#v, %v, want nil components and error", service, worker, err)
			}
		})
	}
}

func TestBuildCatalogMutationComponentsRejectsInvalidConfigWhenCacheDisabled(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*productServiceConfig)
	}{
		{name: "zero delayed delete delay", mutate: func(c *productServiceConfig) { c.CacheInvalidation.DelayedDeleteDelay = 0 }},
		{name: "negative delayed delete delay", mutate: func(c *productServiceConfig) { c.CacheInvalidation.DelayedDeleteDelay = -time.Second }},
		{name: "zero poll interval", mutate: func(c *productServiceConfig) { c.CacheInvalidation.PollInterval = 0 }},
		{name: "negative poll interval", mutate: func(c *productServiceConfig) { c.CacheInvalidation.PollInterval = -time.Second }},
		{name: "zero batch size", mutate: func(c *productServiceConfig) { c.CacheInvalidation.BatchSize = 0 }},
		{name: "negative batch size", mutate: func(c *productServiceConfig) { c.CacheInvalidation.BatchSize = -1 }},
		{name: "zero lease duration", mutate: func(c *productServiceConfig) { c.CacheInvalidation.LeaseDuration = 0 }},
		{name: "negative lease duration", mutate: func(c *productServiceConfig) { c.CacheInvalidation.LeaseDuration = -time.Second }},
		{name: "zero max retries", mutate: func(c *productServiceConfig) { c.CacheInvalidation.MaxRetries = 0 }},
		{name: "negative max retries", mutate: func(c *productServiceConfig) { c.CacheInvalidation.MaxRetries = -1 }},
		{name: "zero retry base delay", mutate: func(c *productServiceConfig) { c.CacheInvalidation.RetryBaseDelay = 0 }},
		{name: "negative retry base delay", mutate: func(c *productServiceConfig) { c.CacheInvalidation.RetryBaseDelay = -time.Second }},
		{name: "zero retry max delay", mutate: func(c *productServiceConfig) { c.CacheInvalidation.RetryMaxDelay = 0 }},
		{name: "negative retry max delay", mutate: func(c *productServiceConfig) { c.CacheInvalidation.RetryMaxDelay = -time.Second }},
		{name: "retry base exceeds max", mutate: func(c *productServiceConfig) {
			c.CacheInvalidation.RetryBaseDelay = c.CacheInvalidation.RetryMaxDelay + time.Second
		}},
		{name: "zero rpc timeout", mutate: func(c *productServiceConfig) { c.Timeout = 0 }},
		{name: "negative rpc timeout", mutate: func(c *productServiceConfig) { c.Timeout = -1 }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := testProductServiceConfig()
			tt.mutate(&config)
			service, worker, err := buildCatalogMutationComponents(nil, nil, config)
			if err == nil || service != nil || worker != nil {
				t.Fatalf("buildCatalogMutationComponents() = %#v, %#v, %v, want nil components and error", service, worker, err)
			}
		})
	}
}

func testProductServiceConfig() productServiceConfig {
	return productServiceConfig{
		RpcServerConf: zrpc.RpcServerConf{Timeout: 2000},
		CacheInvalidation: cacheInvalidationConfig{
			PollInterval:       time.Second,
			BatchSize:          50,
			DelayedDeleteDelay: time.Second,
			LeaseDuration:      30 * time.Second,
			MaxRetries:         5,
			RetryBaseDelay:     time.Second,
			RetryMaxDelay:      30 * time.Second,
		},
		ConfirmationConsumer: confirmationConsumerConfig{CallTimeout: time.Second},
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
