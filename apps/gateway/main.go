package main

import (
	"flag"
	"log"
	"net/http"

	"github.com/yuyu945/AI-Shopping/apps/gateway/internal/handler"
	platformconfig "github.com/yuyu945/AI-Shopping/internal/platform/config"
	"github.com/yuyu945/AI-Shopping/internal/platform/trace"
	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/rest"
)

type gatewayConfig struct {
	rest.RestConf
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

	server := rest.MustNewServer(config.RestConf)
	defer server.Stop()
	server.AddRoute(rest.Route{
		Method:  http.MethodGet,
		Path:    "/healthz",
		Handler: handler.NewHealthHandler(trace.EnsureTraceID),
	})
	server.Start()
}
