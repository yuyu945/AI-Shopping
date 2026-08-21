package main

import (
	"flag"
	"log"
	"net/http"

	"github.com/yuyu945/AI-Shopping/apps/gateway/internal/handler"
	"github.com/yuyu945/AI-Shopping/apps/gateway/internal/middleware"
	"github.com/yuyu945/AI-Shopping/apps/gateway/internal/productclient"
	platformconfig "github.com/yuyu945/AI-Shopping/internal/platform/config"
	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/rest"
	"github.com/zeromicro/go-zero/rest/router"
	"github.com/zeromicro/go-zero/zrpc"
)

type gatewayConfig struct {
	rest.RestConf
	ProductRPC zrpc.RpcClientConf
}

func main() {
	var configFile string
	flag.StringVar(&configFile, "f", "apps/gateway/etc/gateway.yaml", "Gateway configuration file")
	flag.Parse()

	var config gatewayConfig
	if err := conf.Load(configFile, &config); err != nil {
		log.Fatalf("gateway load configuration: %v", err)
	}
	if _, err := platformconfig.Load(); err != nil {
		log.Fatalf("gateway load runtime configuration: %v", err)
	}
	productRPC, err := zrpc.NewClient(config.ProductRPC)
	if err != nil {
		log.Fatalf("gateway connect product service: %v", err)
	}
	defer productRPC.Conn().Close()
	productHandler := handler.NewProductHandler(productclient.NewGRPCClient(productRPC.Conn()))

	server := rest.MustNewServer(config.RestConf, rest.WithRouter(middleware.NewTraceRouter(router.NewRouter())))
	defer server.Stop()
	server.AddRoute(rest.Route{
		Method:  http.MethodGet,
		Path:    "/healthz",
		Handler: handler.NewHealthHandler(nil),
	})
	server.AddRoute(rest.Route{Method: http.MethodGet, Path: "/api/v1/products", Handler: productHandler.List()})
	server.AddRoute(rest.Route{Method: http.MethodGet, Path: "/api/v1/products/:id", Handler: productHandler.Get()})
	server.Start()
}
