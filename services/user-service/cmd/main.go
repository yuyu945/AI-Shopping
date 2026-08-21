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
	userpb "github.com/yuyu945/AI-Shopping/services/user-service/gen"
	userserver "github.com/yuyu945/AI-Shopping/services/user-service/internal/server"
	"github.com/yuyu945/AI-Shopping/services/user-service/internal/user"
	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"
)

const SERVICE_NAME = "user-service"

func main() {
	var configFile string
	flag.StringVar(&configFile, "f", "services/user-service/etc/user-service.yaml", "Service configuration file")
	flag.Parse()

	var config zrpc.RpcServerConf
	err := conf.Load(configFile, &config)
	if err != nil {
		log.Fatalf("%s startup: %v", SERVICE_NAME, err)
	}
	runtimeConfig, err := platformconfig.Load()
	if err != nil {
		log.Fatalf("%s load runtime configuration: %v", SERVICE_NAME, err)
	}

	dsn, err := userDSN(runtimeConfig.MySQLDSN)
	if err != nil {
		log.Fatalf("%s validate database: invalid DSN", SERVICE_NAME)
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		log.Fatalf("%s open database: %v", SERVICE_NAME, err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	err = db.PingContext(ctx)
	cancel()
	if err != nil {
		log.Fatalf("%s ping database: unavailable", SERVICE_NAME)
	}
	manager, err := platformauth.NewManager([]byte(runtimeConfig.JWTSecret))
	if err != nil {
		log.Fatalf("%s jwt configuration: invalid", SERVICE_NAME)
	}
	server, err := zrpc.NewServer(config, func(g *grpc.Server) {
		userpb.RegisterUserServiceServer(g, userserver.NewGRPCServer(user.NewUserService(user.NewRepository(db), nil, manager, time.Now), manager, time.Duration(config.Timeout)*time.Millisecond))
	})
	if err != nil {
		log.Fatalf("%s create rpc server: %v", SERVICE_NAME, err)
	}
	defer server.Stop()
	server.Start()
}

func userDSN(dsn string) (string, error) {
	c, err := mysql.ParseDSN(dsn)
	if err != nil {
		return "", fmt.Errorf("parse mysql dsn: %w", err)
	}
	c.DBName = "user_db"
	return c.FormatDSN(), nil
}
