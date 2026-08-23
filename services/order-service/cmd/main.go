package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	platformauth "github.com/yuyu945/AI-Shopping/internal/platform/auth"
	platformconfig "github.com/yuyu945/AI-Shopping/internal/platform/config"
	orderpb "github.com/yuyu945/AI-Shopping/services/order-service/gen"
	orderclient "github.com/yuyu945/AI-Shopping/services/order-service/internal/client"
	"github.com/yuyu945/AI-Shopping/services/order-service/internal/order"
	"github.com/yuyu945/AI-Shopping/services/order-service/internal/outbox"
	"github.com/yuyu945/AI-Shopping/services/order-service/internal/recovery"
	orderserver "github.com/yuyu945/AI-Shopping/services/order-service/internal/server"
	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"
)

const SERVICE_NAME = "order-service"

type orderServiceConfig struct {
	zrpc.RpcServerConf
	UserRPC            zrpc.RpcClientConf
	ProductRPC         zrpc.RpcClientConf
	ConfirmationOutbox confirmationOutboxConfig
	PaymentRecovery    paymentRecoveryConfig
}

type confirmationOutboxConfig struct {
	PollInterval  time.Duration
	BatchSize     int
	LeaseDuration time.Duration
	CallTimeout   time.Duration
}

type paymentRecoveryConfig struct {
	PollInterval  time.Duration
	BatchSize     int
	LeaseDuration time.Duration
	CallTimeout   time.Duration
}

func (c orderServiceConfig) confirmationOutboxWorkerConfig() outbox.Config {
	return outbox.Config{
		BatchSize:     c.ConfirmationOutbox.BatchSize,
		LeaseDuration: c.ConfirmationOutbox.LeaseDuration,
		CallTimeout:   c.ConfirmationOutbox.CallTimeout,
	}
}

func (c orderServiceConfig) paymentRecoveryWorkerConfig() recovery.Config {
	return recovery.Config{
		BatchSize:     c.PaymentRecovery.BatchSize,
		LeaseDuration: c.PaymentRecovery.LeaseDuration,
		CallTimeout:   c.PaymentRecovery.CallTimeout,
	}
}

func main() {
	var configFile string
	flag.StringVar(&configFile, "f", "services/order-service/etc/order-service.yaml", "Service configuration file")
	flag.Parse()

	var config orderServiceConfig
	if err := conf.Load(configFile, &config); err != nil {
		log.Fatalf("%s startup: %v", SERVICE_NAME, err)
	}
	outboxConfig := config.confirmationOutboxWorkerConfig()
	if err := outboxConfig.Validate(); err != nil {
		log.Fatalf("%s startup: invalid confirmation outbox configuration", SERVICE_NAME)
	}
	recoveryConfig := config.paymentRecoveryWorkerConfig()
	runtimeConfig, err := platformconfig.Load()
	if err != nil {
		log.Fatalf("%s load runtime configuration: %v", SERVICE_NAME, err)
	}
	if err := platformconfig.ValidateInternalServiceToken(runtimeConfig.InternalServiceToken); err != nil {
		log.Fatalf("%s startup: invalid internal service authentication configuration", SERVICE_NAME)
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
	repository := order.NewMySQLRepository(db)
	service := order.NewService(repository, orderclient.NewUserClient(userRPC.Conn(), timeout), orderclient.NewProductClient(productRPC.Conn(), timeout))
	reservations := orderclient.NewReservationClient(productRPC.Conn(), runtimeConfig.InternalServiceToken, timeout)
	payment := order.NewPaymentService(repository, reservations, order.IDGeneratorFunc(uuid.NewString), 5*time.Minute)
	publisher := outbox.NewKafkaPublisher(strings.Split(runtimeConfig.KafkaBrokers, ","))
	defer publisher.Close()
	confirmationWorker := outbox.NewWorker(outbox.NewMySQLRepository(db), publisher, outboxConfig)
	recoveryWorker := recovery.NewWorker(recovery.NewMySQLStore(db), orderclient.NewReservationClient(productRPC.Conn(), runtimeConfig.InternalServiceToken, config.PaymentRecovery.CallTimeout), recovery.PaymentServiceSettler{Service: payment}, recoveryConfig)
	zrpc.DontLogContentForMethod(orderpb.OrderService_CreateOrder_FullMethodName)
	zrpc.DontLogContentForMethod(orderpb.OrderService_PayWallet_FullMethodName)
	zrpc.DontLogContentForMethod(orderpb.OrderService_GetOrder_FullMethodName)
	zrpc.DontLogContentForMethod(orderpb.OrderService_ListOrders_FullMethodName)
	server, err := zrpc.NewServer(config.RpcServerConf, func(g *grpc.Server) {
		orderpb.RegisterOrderServiceServer(g, orderserver.NewGRPCServerWithPaymentAndSettlement(service, payment, repository, manager, timeout, runtimeConfig.InternalServiceToken))
	})
	if err != nil {
		log.Fatalf("%s create rpc server: %v", SERVICE_NAME, err)
	}
	defer server.Stop()
	workerCtx, cancelWorker := context.WithCancel(context.Background())
	defer cancelWorker()
	go func() {
		if err := confirmationWorker.Run(workerCtx, config.ConfirmationOutbox.PollInterval); err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("%s confirmation outbox worker stopped", SERVICE_NAME)
		}
	}()
	go func() {
		if err := recoveryWorker.Run(workerCtx, config.PaymentRecovery.PollInterval); err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("%s payment recovery worker stopped", SERVICE_NAME)
		}
	}()
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
