package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"time"

	"github.com/go-sql-driver/mysql"
	platformauth "github.com/yuyu945/AI-Shopping/internal/platform/auth"
	platformconfig "github.com/yuyu945/AI-Shopping/internal/platform/config"
	orderpb "github.com/yuyu945/AI-Shopping/services/order-service/gen"
	orderclient "github.com/yuyu945/AI-Shopping/services/order-service/internal/client"
	"github.com/yuyu945/AI-Shopping/services/order-service/internal/order"
	orderserver "github.com/yuyu945/AI-Shopping/services/order-service/internal/server"
	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"
)

const SERVICE_NAME = "order-service"

type orderServiceConfig struct {
	zrpc.RpcServerConf
	UserRPC    zrpc.RpcClientConf
	ProductRPC zrpc.RpcClientConf
}

func main() {
	var configFile string
	flag.StringVar(&configFile, "f", "services/order-service/etc/order-service.yaml", "Service configuration file")
	flag.Parse()

	var config orderServiceConfig
	if err := conf.Load(configFile, &config); err != nil {
		log.Fatalf("%s startup: %v", SERVICE_NAME, err)
	}
	runtimeConfig, err := platformconfig.Load()
	if err != nil {
		log.Fatalf("%s load runtime configuration: %v", SERVICE_NAME, err)
	}
	dsn, err := tradeDSN(runtimeConfig.MySQLDSN)
	if err != nil {
		log.Fatalf("%s validate database: invalid DSN", SERVICE_NAME)
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		log.Fatalf("%s open database: %v", SERVICE_NAME, err)
	}
	defer db.Close()
	pingCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	err = db.PingContext(pingCtx)
	cancel()
	if err != nil {
		log.Fatalf("%s ping database: unavailable", SERVICE_NAME)
	}
	manager, err := platformauth.NewManager([]byte(runtimeConfig.JWTSecret))
	if err != nil {
		log.Fatalf("%s jwt configuration: invalid", SERVICE_NAME)
	}
	userRPC, err := zrpc.NewClient(config.UserRPC)
	if err != nil {
		log.Fatalf("%s connect user service: %v", SERVICE_NAME, err)
	}
	defer userRPC.Conn().Close()
	productRPC, err := zrpc.NewClient(config.ProductRPC)
	if err != nil {
		log.Fatalf("%s connect product service: %v", SERVICE_NAME, err)
	}
	defer productRPC.Conn().Close()
	timeout := time.Duration(config.Timeout) * time.Millisecond
	service := order.NewService(order.NewMySQLRepository(db), orderclient.NewUserClient(userRPC.Conn(), timeout), orderclient.NewProductClient(productRPC.Conn(), timeout))
	zrpc.DontLogContentForMethod(orderpb.OrderService_CreateOrder_FullMethodName)
	zrpc.DontLogContentForMethod(orderpb.OrderService_GetOrder_FullMethodName)
	zrpc.DontLogContentForMethod(orderpb.OrderService_ListOrders_FullMethodName)
	server, err := zrpc.NewServer(config.RpcServerConf, func(g *grpc.Server) {
		orderpb.RegisterOrderServiceServer(g, orderserver.NewGRPCServer(service, manager, timeout))
	})
	if err != nil {
		log.Fatalf("%s create rpc server: %v", SERVICE_NAME, err)
	}
	defer server.Stop()
	server.Start()
}

func tradeDSN(dsn string) (string, error) {
	c, err := mysql.ParseDSN(dsn)
	if err != nil {
		return "", fmt.Errorf("parse mysql dsn: %w", err)
	}
	c.DBName = "trade_db"
	return c.FormatDSN(), nil
}
