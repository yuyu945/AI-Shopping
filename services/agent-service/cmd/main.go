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
	platformauth "github.com/yuyu945/AI-Shopping/internal/platform/auth"
	platformconfig "github.com/yuyu945/AI-Shopping/internal/platform/config"
	agentpb "github.com/yuyu945/AI-Shopping/services/agent-service/gen"
	"github.com/yuyu945/AI-Shopping/services/agent-service/internal/agent"
	agentclient "github.com/yuyu945/AI-Shopping/services/agent-service/internal/client"
	agentserver "github.com/yuyu945/AI-Shopping/services/agent-service/internal/server"
	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"
)

const SERVICE_NAME = "agent-service"

type agentServiceConfig struct {
	zrpc.RpcServerConf
	Runtime      agentRuntimeConfig
	ProductRPC   zrpc.RpcClientConf
	UserRPC      zrpc.RpcClientConf
	KnowledgeRPC zrpc.RpcClientConf
}

type agentRuntimeConfig struct {
	MaxSteps    uint32
	RunTimeout  time.Duration
	ToolTimeout time.Duration
}

func main() {
	var configFile string
	flag.StringVar(&configFile, "f", "services/agent-service/etc/agent-service.yaml", "Service configuration file")
	flag.Parse()

	config, err := loadAgentServiceConfig(configFile)
	if err != nil {
		log.Fatalf("%s startup: %v", SERVICE_NAME, err)
	}
	runtimeConfig, err := platformconfig.Load()
	if err != nil {
		log.Fatalf("%s load runtime configuration: %v", SERVICE_NAME, err)
	}
	dsn, err := agentDSN(runtimeConfig.MySQLDSN)
	if err != nil {
		log.Fatalf("%s validate database: invalid DSN", SERVICE_NAME)
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		log.Fatalf("%s open database: %v", SERVICE_NAME, err)
	}
	defer db.Close()
	pingCtx, cancelPing := context.WithTimeout(context.Background(), 5*time.Second)
	err = db.PingContext(pingCtx)
	cancelPing()
	if err != nil {
		log.Fatalf("%s ping database: unavailable", SERVICE_NAME)
	}
	manager, err := platformauth.NewManager([]byte(runtimeConfig.JWTSecret))
	if err != nil {
		log.Fatalf("%s jwt configuration: invalid", SERVICE_NAME)
	}
	productRPC, err := zrpc.NewClient(config.ProductRPC)
	if err != nil {
		log.Fatalf("%s connect product service: %v", SERVICE_NAME, err)
	}
	defer productRPC.Conn().Close()
	userRPC, err := zrpc.NewClient(config.UserRPC)
	if err != nil {
		log.Fatalf("%s connect user service: %v", SERVICE_NAME, err)
	}
	defer userRPC.Conn().Close()
	knowledgeRPC, err := zrpc.NewClient(config.KnowledgeRPC)
	if err != nil {
		log.Fatalf("%s connect knowledge service: %v", SERVICE_NAME, err)
	}
	defer knowledgeRPC.Conn().Close()

	timeout := time.Duration(config.Timeout) * time.Millisecond
	repository := agent.NewMySQLRepository(db)
	registry := agent.NewDefaultToolRegistry(config.Runtime.ToolTimeout)
	tools := agent.NewToolExecutor(
		registry,
		agentclient.NewProductClient(productRPC.Conn(), config.Runtime.ToolTimeout),
		agentclient.NewUserClient(userRPC.Conn(), config.Runtime.ToolTimeout),
		agentclient.NewKnowledgeClient(knowledgeRPC.Conn(), config.Runtime.ToolTimeout),
	)
	runService := agent.NewRunService(repository, disabledModel{}, registry, tools, agent.RunServiceOptions{
		MaxSteps: config.Runtime.MaxSteps, RunTimeout: config.Runtime.RunTimeout, Now: time.Now,
	})

	zrpc.DontLogContentForMethod(agentpb.AgentService_StartRun_FullMethodName)
	server, err := zrpc.NewServer(config.RpcServerConf, func(g *grpc.Server) {
		agentpb.RegisterAgentServiceServer(g, agentserver.NewGRPCServer(runService, repository, manager, timeout))
	})
	if err != nil {
		log.Fatalf("%s create rpc server: %v", SERVICE_NAME, err)
	}
	defer server.Stop()
	server.Start()
}

func loadAgentServiceConfig(path string) (agentServiceConfig, error) {
	var config agentServiceConfig
	if err := conf.Load(path, &config); err != nil {
		return agentServiceConfig{}, fmt.Errorf("%s load configuration: %w", SERVICE_NAME, err)
	}
	if config.Name != SERVICE_NAME || config.ListenOn == "" {
		return agentServiceConfig{}, errors.New("invalid Name or ListenOn")
	}
	if config.Runtime.MaxSteps == 0 {
		return agentServiceConfig{}, errors.New("runtime max steps must be positive")
	}
	if config.Runtime.RunTimeout <= 0 {
		return agentServiceConfig{}, errors.New("runtime run timeout must be positive")
	}
	if config.Runtime.ToolTimeout <= 0 {
		return agentServiceConfig{}, errors.New("runtime tool timeout must be positive")
	}
	return config, nil
}

type disabledModel struct{}

func (disabledModel) Next(context.Context, agent.ModelInput) (agent.ModelOutput, error) {
	return agent.ModelOutput{}, fmt.Errorf("%w: model provider is not configured", agent.ErrModelFailed)
}

func agentDSN(dsn string) (string, error) {
	config, err := mysql.ParseDSN(dsn)
	if err != nil {
		return "", fmt.Errorf("parse mysql dsn: %w", err)
	}
	config.DBName = "agent_db"
	return config.FormatDSN(), nil
}
