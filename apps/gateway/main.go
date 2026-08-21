package main

import (
	"flag"
	"log"
	"net/http"

	"github.com/yuyu945/AI-Shopping/apps/gateway/internal/handler"
	"github.com/yuyu945/AI-Shopping/apps/gateway/internal/middleware"
	"github.com/yuyu945/AI-Shopping/apps/gateway/internal/productclient"
	"github.com/yuyu945/AI-Shopping/apps/gateway/internal/userclient"
	platformauth "github.com/yuyu945/AI-Shopping/internal/platform/auth"
	platformconfig "github.com/yuyu945/AI-Shopping/internal/platform/config"
	userpb "github.com/yuyu945/AI-Shopping/services/user-service/gen"
	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/rest"
	"github.com/zeromicro/go-zero/rest/router"
	"github.com/zeromicro/go-zero/zrpc"
)

type gatewayConfig struct {
	rest.RestConf
	ProductRPC zrpc.RpcClientConf
	UserRPC    zrpc.RpcClientConf
}

func main() {
	var configFile string
	flag.StringVar(&configFile, "f", "apps/gateway/etc/gateway.yaml", "Gateway configuration file")
	flag.Parse()

	var config gatewayConfig
	if err := conf.Load(configFile, &config); err != nil {
		log.Fatalf("gateway load configuration: %v", err)
	}
	runtimeConfig, err := platformconfig.Load()
	if err != nil {
		log.Fatalf("gateway load runtime configuration: %v", err)
	}
	productRPC, err := zrpc.NewClient(config.ProductRPC)
	if err != nil {
		log.Fatalf("gateway connect product service: %v", err)
	}
	defer productRPC.Conn().Close()
	userRPC, err := zrpc.NewClient(config.UserRPC)
	if err != nil {
		log.Fatalf("gateway connect user service: %v", err)
	}
	defer userRPC.Conn().Close()
	zrpc.DontLogClientContentForMethod(userpb.UserService_Register_FullMethodName)
	zrpc.DontLogClientContentForMethod(userpb.UserService_Login_FullMethodName)
	manager, err := platformauth.NewManager([]byte(runtimeConfig.JWTSecret))
	if err != nil {
		log.Fatalf("gateway jwt configuration: invalid")
	}
	productHandler := handler.NewProductHandler(productclient.NewGRPCClient(productRPC.Conn()))
	userHandler := handler.NewUserHandler(userclient.NewGRPCClient(userRPC.Conn()))
	authMiddleware := middleware.NewAuthMiddleware(manager)

	server := rest.MustNewServer(config.RestConf, rest.WithRouter(middleware.NewTraceRouter(router.NewRouter())))
	defer server.Stop()
	server.AddRoute(rest.Route{
		Method:  http.MethodGet,
		Path:    "/healthz",
		Handler: handler.NewHealthHandler(nil),
	})
	server.AddRoute(rest.Route{Method: http.MethodGet, Path: "/api/v1/products", Handler: productHandler.List()})
	server.AddRoute(rest.Route{Method: http.MethodGet, Path: "/api/v1/products/:id", Handler: productHandler.Get()})
	server.AddRoute(rest.Route{Method: http.MethodPost, Path: "/api/v1/auth/register", Handler: userHandler.Register()})
	server.AddRoute(rest.Route{Method: http.MethodPost, Path: "/api/v1/auth/login", Handler: userHandler.Login()})
	server.AddRoute(rest.Route{Method: http.MethodGet, Path: "/api/v1/users/me", Handler: authMiddleware.Wrap(userHandler.Profile())})
	server.AddRoute(rest.Route{Method: http.MethodPut, Path: "/api/v1/users/me/profile", Handler: authMiddleware.Wrap(userHandler.Profile())})
	server.AddRoute(rest.Route{Method: http.MethodGet, Path: "/api/v1/users/me/addresses", Handler: authMiddleware.Wrap(userHandler.Addresses())})
	server.AddRoute(rest.Route{Method: http.MethodPost, Path: "/api/v1/users/me/addresses", Handler: authMiddleware.Wrap(userHandler.Addresses())})
	server.AddRoute(rest.Route{Method: http.MethodPut, Path: "/api/v1/users/me/addresses/:id", Handler: authMiddleware.Wrap(userHandler.Address())})
	server.AddRoute(rest.Route{Method: http.MethodDelete, Path: "/api/v1/users/me/addresses/:id", Handler: authMiddleware.Wrap(userHandler.Address())})
	server.Start()
}
