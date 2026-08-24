package main

import (
	"flag"
	"log"
	"net/http"

	"github.com/yuyu945/AI-Shopping/apps/gateway/internal/agentclient"
	"github.com/yuyu945/AI-Shopping/apps/gateway/internal/handler"
	"github.com/yuyu945/AI-Shopping/apps/gateway/internal/knowledgeclient"
	"github.com/yuyu945/AI-Shopping/apps/gateway/internal/middleware"
	"github.com/yuyu945/AI-Shopping/apps/gateway/internal/orderclient"
	"github.com/yuyu945/AI-Shopping/apps/gateway/internal/productclient"
	"github.com/yuyu945/AI-Shopping/apps/gateway/internal/userclient"
	platformauth "github.com/yuyu945/AI-Shopping/internal/platform/auth"
	platformconfig "github.com/yuyu945/AI-Shopping/internal/platform/config"
	agentpb "github.com/yuyu945/AI-Shopping/services/agent-service/gen"
	knowledgepb "github.com/yuyu945/AI-Shopping/services/knowledge-service/gen"
	orderpb "github.com/yuyu945/AI-Shopping/services/order-service/gen"
	userpb "github.com/yuyu945/AI-Shopping/services/user-service/gen"
	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/rest"
	"github.com/zeromicro/go-zero/rest/router"
	"github.com/zeromicro/go-zero/zrpc"
)

type gatewayConfig struct {
	rest.RestConf
	ProductRPC   zrpc.RpcClientConf
	UserRPC      zrpc.RpcClientConf
	OrderRPC     zrpc.RpcClientConf
	KnowledgeRPC zrpc.RpcClientConf
	AgentRPC     zrpc.RpcClientConf
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
	orderRPC, err := zrpc.NewClient(config.OrderRPC)
	if err != nil {
		log.Fatalf("gateway connect order service: %v", err)
	}
	defer orderRPC.Conn().Close()
	knowledgeRPC, err := zrpc.NewClient(config.KnowledgeRPC)
	if err != nil {
		log.Fatalf("gateway connect knowledge service: %v", err)
	}
	defer knowledgeRPC.Conn().Close()
	agentRPC, err := zrpc.NewClient(config.AgentRPC)
	if err != nil {
		log.Fatalf("gateway connect agent service: %v", err)
	}
	defer agentRPC.Conn().Close()
	zrpc.DontLogClientContentForMethod(userpb.UserService_Register_FullMethodName)
	zrpc.DontLogClientContentForMethod(userpb.UserService_Login_FullMethodName)
	zrpc.DontLogClientContentForMethod(orderpb.OrderService_CreateOrder_FullMethodName)
	zrpc.DontLogClientContentForMethod(orderpb.OrderService_PayWallet_FullMethodName)
	zrpc.DontLogClientContentForMethod(orderpb.OrderService_GetOrder_FullMethodName)
	zrpc.DontLogClientContentForMethod(orderpb.OrderService_ListOrders_FullMethodName)
	zrpc.DontLogClientContentForMethod(orderpb.OrderService_SubmitReview_FullMethodName)
	zrpc.DontLogClientContentForMethod(orderpb.OrderService_GetAnalyticsOverview_FullMethodName)
	zrpc.DontLogClientContentForMethod(knowledgepb.KnowledgeService_UploadDocument_FullMethodName)
	zrpc.DontLogClientContentForMethod(knowledgepb.KnowledgeService_ListDocuments_FullMethodName)
	zrpc.DontLogClientContentForMethod(knowledgepb.KnowledgeService_GetDocument_FullMethodName)
	zrpc.DontLogClientContentForMethod(knowledgepb.KnowledgeService_RetryDocument_FullMethodName)
	zrpc.DontLogClientContentForMethod(agentpb.AgentService_StartRun_FullMethodName)
	zrpc.DontLogClientContentForMethod(agentpb.AgentService_GetRun_FullMethodName)
	zrpc.DontLogClientContentForMethod(agentpb.AgentService_ListRuns_FullMethodName)
	zrpc.DontLogClientContentForMethod(agentpb.AgentService_GetRunOps_FullMethodName)
	manager, err := platformauth.NewManager([]byte(runtimeConfig.JWTSecret))
	if err != nil {
		log.Fatalf("gateway jwt configuration: invalid")
	}
	productHandler := handler.NewProductHandler(productclient.NewGRPCClient(productRPC.Conn()))
	userHandler := handler.NewUserHandler(userclient.NewGRPCClient(userRPC.Conn()))
	orderHandler := handler.NewOrderHandler(orderclient.NewGRPCClient(orderRPC.Conn()))
	knowledgeHandler := handler.NewKnowledgeHandler(knowledgeclient.NewGRPCClient(knowledgeRPC.Conn()))
	agentHandler := handler.NewAgentHandler(agentclient.NewGRPCClient(agentRPC.Conn()))
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
	server.AddRoute(rest.Route{Method: http.MethodPost, Path: "/api/v1/products/:product_id/knowledge/questions", Handler: authMiddleware.Wrap(knowledgeHandler.ProductQuestion())})
	server.AddRoute(rest.Route{Method: http.MethodPost, Path: "/api/v1/auth/register", Handler: userHandler.Register()})
	server.AddRoute(rest.Route{Method: http.MethodPost, Path: "/api/v1/auth/login", Handler: userHandler.Login()})
	server.AddRoute(rest.Route{Method: http.MethodGet, Path: "/api/v1/users/me", Handler: authMiddleware.Wrap(userHandler.Profile())})
	server.AddRoute(rest.Route{Method: http.MethodPut, Path: "/api/v1/users/me/profile", Handler: authMiddleware.Wrap(userHandler.Profile())})
	server.AddRoute(rest.Route{Method: http.MethodGet, Path: "/api/v1/users/me/addresses", Handler: authMiddleware.Wrap(userHandler.Addresses())})
	server.AddRoute(rest.Route{Method: http.MethodPost, Path: "/api/v1/users/me/addresses", Handler: authMiddleware.Wrap(userHandler.Addresses())})
	server.AddRoute(rest.Route{Method: http.MethodPut, Path: "/api/v1/users/me/addresses/:id", Handler: authMiddleware.Wrap(userHandler.Address())})
	server.AddRoute(rest.Route{Method: http.MethodDelete, Path: "/api/v1/users/me/addresses/:id", Handler: authMiddleware.Wrap(userHandler.Address())})
	server.AddRoute(rest.Route{Method: http.MethodGet, Path: "/api/v1/cart", Handler: authMiddleware.Wrap(orderHandler.Cart())})
	server.AddRoute(rest.Route{Method: http.MethodPost, Path: "/api/v1/cart/items", Handler: authMiddleware.Wrap(orderHandler.Cart())})
	server.AddRoute(rest.Route{Method: http.MethodPut, Path: "/api/v1/cart/items/:id", Handler: authMiddleware.Wrap(orderHandler.CartItem())})
	server.AddRoute(rest.Route{Method: http.MethodDelete, Path: "/api/v1/cart/items/:id", Handler: authMiddleware.Wrap(orderHandler.CartItem())})
	server.AddRoute(rest.Route{Method: http.MethodGet, Path: "/api/v1/orders", Handler: authMiddleware.Wrap(orderHandler.Orders())})
	server.AddRoute(rest.Route{Method: http.MethodPost, Path: "/api/v1/orders", Handler: authMiddleware.Wrap(orderHandler.Orders())})
	server.AddRoute(rest.Route{Method: http.MethodGet, Path: "/api/v1/orders/:order_no", Handler: authMiddleware.Wrap(orderHandler.Order())})
	server.AddRoute(rest.Route{Method: http.MethodPost, Path: "/api/v1/orders/:order_no/payments/wallet", Handler: authMiddleware.Wrap(orderHandler.WalletPayment())})
	server.AddRoute(rest.Route{Method: http.MethodPost, Path: "/api/v1/orders/:order_no/items/:sku_id/reviews", Handler: authMiddleware.Wrap(orderHandler.Review())})
	server.AddRoute(rest.Route{Method: http.MethodPost, Path: "/api/v1/knowledge/documents", Handler: authMiddleware.Wrap(knowledgeHandler.Documents())})
	server.AddRoute(rest.Route{Method: http.MethodGet, Path: "/api/v1/ops/knowledge/documents", Handler: authMiddleware.Wrap(handler.RequireOperatorHeader(knowledgeHandler.OpsDocuments()))})
	server.AddRoute(rest.Route{Method: http.MethodPost, Path: "/api/v1/ops/knowledge/documents", Handler: authMiddleware.Wrap(handler.RequireOperatorHeader(knowledgeHandler.OpsDocuments()))})
	server.AddRoute(rest.Route{Method: http.MethodGet, Path: "/api/v1/ops/knowledge/documents/:document_no", Handler: authMiddleware.Wrap(handler.RequireOperatorHeader(knowledgeHandler.OpsDocument()))})
	server.AddRoute(rest.Route{Method: http.MethodPost, Path: "/api/v1/ops/knowledge/documents/:document_no/retry", Handler: authMiddleware.Wrap(handler.RequireOperatorHeader(knowledgeHandler.OpsRetryDocument()))})
	server.AddRoute(rest.Route{Method: http.MethodPost, Path: "/api/v1/agent/runs", Handler: authMiddleware.Wrap(agentHandler.Runs())})
	server.AddRoute(rest.Route{Method: http.MethodGet, Path: "/api/v1/agent/runs/:run_id", Handler: authMiddleware.Wrap(agentHandler.Run())})
	server.AddRoute(rest.Route{Method: http.MethodGet, Path: "/api/v1/agent/runs/:run_id/events", Handler: authMiddleware.Wrap(agentHandler.Events())})
	server.AddRoute(rest.Route{Method: http.MethodGet, Path: "/api/v1/ops/agent-runs", Handler: authMiddleware.Wrap(handler.RequireOperatorHeader(agentHandler.OpsRuns()))})
	server.AddRoute(rest.Route{Method: http.MethodGet, Path: "/api/v1/ops/agent-runs/:run_id", Handler: authMiddleware.Wrap(handler.RequireOperatorHeader(agentHandler.OpsRun()))})
	server.AddRoute(rest.Route{Method: http.MethodGet, Path: "/api/v1/ops/events/overview", Handler: authMiddleware.Wrap(handler.RequireOperatorHeader(orderHandler.OpsEventsOverview()))})
	server.Start()
}
