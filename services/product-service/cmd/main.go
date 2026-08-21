package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/redis/go-redis/v9"
	platformconfig "github.com/yuyu945/AI-Shopping/internal/platform/config"
	productpb "github.com/yuyu945/AI-Shopping/services/product-service/gen"
	"github.com/yuyu945/AI-Shopping/services/product-service/internal/catalog"
	productserver "github.com/yuyu945/AI-Shopping/services/product-service/internal/server"
	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"
)

const SERVICE_NAME = "product-service"

type productServiceConfig struct {
	zrpc.RpcServerConf
	CacheInvalidation catalog.CacheInvalidationConfig
}

func main() {
	var configFile string
	flag.StringVar(&configFile, "f", "services/product-service/etc/product-service.yaml", "Service configuration file")
	flag.Parse()

	var config productServiceConfig
	if err := conf.Load(configFile, &config); err != nil {
		log.Fatalf("%s startup: %v", SERVICE_NAME, err)
	}
	if config.Name != SERVICE_NAME || config.ListenOn == "" {
		log.Fatalf("%s startup: invalid Name or ListenOn", SERVICE_NAME)
	}
	runtimeConfig, err := platformconfig.Load()
	if err != nil {
		log.Fatalf("%s load runtime configuration: %v", SERVICE_NAME, err)
	}

	dsn, err := catalogDSN(runtimeConfig.MySQLDSN)
	if err != nil {
		log.Fatalf("%s validate catalog database configuration: invalid DSN", SERVICE_NAME)
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		log.Fatalf("%s open catalog database: %v", SERVICE_NAME, err)
	}
	defer db.Close()
	pingCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	err = db.PingContext(pingCtx)
	cancel()
	if err != nil {
		log.Fatalf("%s ping catalog database: database unavailable", SERVICE_NAME)
	}
	detailCache, closeCache, cacheErr := buildDetailCache(context.Background(), runtimeConfig.RedisAddr, 5*time.Second, newRedisCacheClient)
	if cacheErr != nil {
		log.Printf("%s product cache disabled: dependency unavailable", SERVICE_NAME)
	} else {
		defer closeCache()
	}

	catalogRepository := catalog.NewRepository(db)
	productService := catalog.NewProductService(catalogRepository, detailCache)
	mutationRepository := catalog.NewMutationRepository(db)
	if _, err := catalog.NewCatalogMutationService(
		mutationRepository,
		detailCache,
		time.Now,
		config.CacheInvalidation.RetryBaseDelay,
		config.CacheInvalidation.CallTimeout,
	); err != nil {
		log.Fatalf("%s startup: invalid cache invalidation configuration", SERVICE_NAME)
	}
	worker, err := buildCacheInvalidationWorker(db, detailCache, config.CacheInvalidation)
	if err != nil {
		log.Fatalf("%s startup: invalid cache invalidation configuration", SERVICE_NAME)
	}

	rpcServer, err := zrpc.NewServer(config.RpcServerConf, func(server *grpc.Server) {
		productpb.RegisterProductServiceServer(server, productserver.NewGRPCServer(productService, time.Duration(config.Timeout)*time.Millisecond))
	})
	if err != nil {
		log.Fatalf("%s create rpc server: %v", SERVICE_NAME, err)
	}
	defer rpcServer.Stop()
	if worker != nil {
		workerCtx, cancelWorker := context.WithCancel(context.Background())
		defer cancelWorker()
		go func() {
			if err := worker.Run(workerCtx); err != nil && !errors.Is(err, context.Canceled) {
				log.Printf("%s cache invalidation worker stopped", SERVICE_NAME)
			}
		}()
	}
	rpcServer.Start()
}

type redisOptions struct {
	Addr    string
	Timeout time.Duration
}

type cacheClient interface {
	Ping(context.Context) error
	Close() error
	DetailCache() catalog.DetailCache
}

type redisCacheClient struct{ client *redis.Client }

func newRedisCacheClient(options redisOptions) cacheClient {
	return &redisCacheClient{client: redis.NewClient(&redis.Options{
		Addr: options.Addr, DialTimeout: options.Timeout, ReadTimeout: options.Timeout, WriteTimeout: options.Timeout,
	})}
}

func (c *redisCacheClient) Ping(ctx context.Context) error { return c.client.Ping(ctx).Err() }
func (c *redisCacheClient) Close() error                   { return c.client.Close() }
func (c *redisCacheClient) DetailCache() catalog.DetailCache {
	return catalog.NewRedisDetailCache(c.client)
}

func buildDetailCache(ctx context.Context, address string, timeout time.Duration, newClient func(redisOptions) cacheClient) (catalog.DetailCache, func(), error) {
	client := newClient(redisOptions{Addr: address, Timeout: timeout})
	pingCtx, cancel := context.WithTimeout(ctx, timeout)
	err := client.Ping(pingCtx)
	cancel()
	if err != nil {
		_ = client.Close()
		return nil, nil, fmt.Errorf("product cache unavailable")
	}
	return client.DetailCache(), func() { _ = client.Close() }, nil
}

func buildCacheInvalidationWorker(db *sql.DB, detailCache catalog.DetailCache, config catalog.CacheInvalidationConfig) (*catalog.CacheInvalidationWorker, error) {
	if detailCache == nil {
		return nil, nil
	}
	return catalog.NewCacheInvalidationWorker(catalog.NewCacheInvalidationRepository(db), detailCache, config)
}

func catalogDSN(dsn string) (string, error) {
	config, err := mysql.ParseDSN(dsn)
	if err != nil {
		return "", fmt.Errorf("parse mysql dsn: %w", err)
	}
	if config.DBName == "" {
		return "", fmt.Errorf("missing mysql database")
	}
	config.DBName = "catalog_db"
	return config.FormatDSN(), nil
}
