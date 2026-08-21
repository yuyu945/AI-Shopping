package main

import (
	"context"
	"database/sql"
	"flag"
	"log"
	"time"

	_ "github.com/go-sql-driver/mysql"
	platformconfig "github.com/yuyu945/AI-Shopping/internal/platform/config"
	productpb "github.com/yuyu945/AI-Shopping/services/product-service/gen"
	"github.com/yuyu945/AI-Shopping/services/product-service/internal/catalog"
	productserver "github.com/yuyu945/AI-Shopping/services/product-service/internal/server"
	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"
)

const SERVICE_NAME = "product-service"

func main() {
	var configFile string
	flag.StringVar(&configFile, "f", "services/product-service/etc/product-service.yaml", "Service configuration file")
	flag.Parse()

	var config zrpc.RpcServerConf
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

	db, err := sql.Open("mysql", runtimeConfig.MySQLDSN)
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

	catalogRepository := catalog.NewRepository(db)
	productService := catalog.NewProductService(catalogRepository, nil)
	rpcServer, err := zrpc.NewServer(config, func(server *grpc.Server) {
		productpb.RegisterProductServiceServer(server, productserver.NewGRPCServer(productService, time.Duration(config.Timeout)*time.Millisecond))
	})
	if err != nil {
		log.Fatalf("%s create rpc server: %v", SERVICE_NAME, err)
	}
	defer rpcServer.Stop()
	rpcServer.Start()
}
