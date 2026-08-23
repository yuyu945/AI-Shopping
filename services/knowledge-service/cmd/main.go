package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	platformauth "github.com/yuyu945/AI-Shopping/internal/platform/auth"
	platformconfig "github.com/yuyu945/AI-Shopping/internal/platform/config"
	knowledgepb "github.com/yuyu945/AI-Shopping/services/knowledge-service/gen"
	"github.com/yuyu945/AI-Shopping/services/knowledge-service/internal/knowledge"
	"github.com/yuyu945/AI-Shopping/services/knowledge-service/internal/outbox"
	knowledgeserver "github.com/yuyu945/AI-Shopping/services/knowledge-service/internal/server"
	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"
)

const SERVICE_NAME = "knowledge-service"

type knowledgeServiceConfig struct {
	zrpc.RpcServerConf
	Upload      uploadConfig
	Outbox      outboxConfig
	Embedding   embeddingConfig
	VectorStore vectorStoreConfig
}

type uploadConfig struct {
	Bucket       string
	MaxFileBytes uint64
	MinIOSecure  bool
}

type outboxConfig struct {
	PollInterval  time.Duration
	BatchSize     int
	LeaseDuration time.Duration
	CallTimeout   time.Duration
	MaxAttempts   int
}

type embeddingConfig struct {
	Provider  string
	Model     string
	Dimension int
	Timeout   time.Duration
}

type vectorStoreConfig struct {
	Backend    string
	Collection string
	Timeout    time.Duration
}

func (c knowledgeServiceConfig) validate() error {
	if c.Name != SERVICE_NAME || c.ListenOn == "" {
		return errors.New("invalid Name or ListenOn")
	}
	if strings.TrimSpace(c.Upload.Bucket) == "" {
		return errors.New("upload bucket must be configured")
	}
	if c.Upload.MaxFileBytes == 0 {
		return errors.New("upload max file bytes must be positive")
	}
	if c.Outbox.PollInterval <= 0 {
		return errors.New("outbox poll interval must be positive")
	}
	if strings.TrimSpace(c.Embedding.Provider) == "" || strings.TrimSpace(c.Embedding.Model) == "" || c.Embedding.Dimension <= 0 || c.Embedding.Timeout <= 0 {
		return errors.New("embedding config is invalid")
	}
	if strings.TrimSpace(c.VectorStore.Backend) == "" || strings.TrimSpace(c.VectorStore.Collection) == "" || c.VectorStore.Timeout <= 0 {
		return errors.New("vector store config is invalid")
	}
	return c.outboxWorkerConfig().Validate()
}

func (c knowledgeServiceConfig) uploadServiceConfig() knowledge.Config {
	return knowledge.Config{Bucket: c.Upload.Bucket, MaxFileBytes: c.Upload.MaxFileBytes}
}

func (c knowledgeServiceConfig) outboxWorkerConfig() outbox.Config {
	return outbox.Config{
		BatchSize:     c.Outbox.BatchSize,
		LeaseDuration: c.Outbox.LeaseDuration,
		CallTimeout:   c.Outbox.CallTimeout,
		MaxAttempts:   c.Outbox.MaxAttempts,
	}
}

func (c knowledgeServiceConfig) retrievalConfig() knowledge.RetrievalConfig {
	return knowledge.RetrievalConfig{Model: c.Embedding.Model, DefaultTopK: 5, MaxTopK: 10}
}

func (c knowledgeServiceConfig) embedConfig() knowledge.EmbedConfig {
	return knowledge.EmbedConfig{Model: c.Embedding.Model, Dimension: c.Embedding.Dimension, MaxBatchSize: 10}
}

func main() {
	var configFile string
	flag.StringVar(&configFile, "f", "services/knowledge-service/etc/knowledge-service.yaml", "Service configuration file")
	flag.Parse()

	var config knowledgeServiceConfig
	if err := conf.Load(configFile, &config); err != nil {
		log.Fatalf("%s startup: %v", SERVICE_NAME, err)
	}
	if err := config.validate(); err != nil {
		log.Fatalf("%s startup: invalid configuration", SERVICE_NAME)
	}
	runtimeConfig, err := platformconfig.Load()
	if err != nil {
		log.Fatalf("%s load runtime configuration: %v", SERVICE_NAME, err)
	}
	dsn, err := knowledgeDSN(runtimeConfig.MySQLDSN)
	if err != nil {
		log.Fatalf("%s validate database configuration: invalid DSN", SERVICE_NAME)
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
	storage, err := knowledge.NewMinIOStorage(runtimeConfig.MinIOEndpoint, runtimeConfig.MinIOAccessKey, runtimeConfig.MinIOSecretKey, config.Upload.MinIOSecure)
	if err != nil {
		log.Fatalf("%s minio configuration: invalid", SERVICE_NAME)
	}
	repository := knowledge.NewMySQLRepository(db)
	service := knowledge.NewService(repository, storage, knowledge.IDGeneratorFunc(uuid.NewString), time.Now, config.uploadServiceConfig())
	embeddingProvider, err := buildEmbeddingProvider(config.Embedding, runtimeConfig)
	if err != nil {
		log.Fatalf("%s embedding configuration: invalid", SERVICE_NAME)
	}
	vectorStore, err := buildVectorStore(context.Background(), config.VectorStore, runtimeConfig, config.Embedding.Dimension)
	if err != nil {
		log.Fatalf("%s vector store configuration: invalid", SERVICE_NAME)
	}
	ingestService := knowledge.NewIngestService(repository, storage, knowledge.IDGeneratorFunc(uuid.NewString), time.Now, knowledge.IngestConfig{
		Bucket:             config.Upload.Bucket,
		EmbeddingModel:     config.Embedding.Model,
		EmbeddingDimension: config.Embedding.Dimension,
		Chunker:            knowledge.Chunker{TargetChars: 900, MaxChars: 1200},
	})
	embedService := knowledge.NewEmbedService(repository, embeddingProvider, vectorStore, config.embedConfig())
	retrieval := knowledge.NewRetrievalService(repository, embeddingProvider, vectorStore, config.retrievalConfig())
	publisher := outbox.NewKafkaPublisher(strings.Split(runtimeConfig.KafkaBrokers, ","))
	defer publisher.Close()
	worker := outbox.NewWorker(outbox.NewMySQLRepository(db), publisher, config.outboxWorkerConfig())
	ingestConsumer := knowledge.NewKafkaDocumentIngestConsumer(strings.Split(runtimeConfig.KafkaBrokers, ","), ingestService, config.Outbox.CallTimeout)
	defer ingestConsumer.Close()
	embedConsumer := knowledge.NewKafkaChunkEmbedConsumer(strings.Split(runtimeConfig.KafkaBrokers, ","), embedService, config.Outbox.CallTimeout)
	defer embedConsumer.Close()
	zrpc.DontLogContentForMethod(knowledgepb.KnowledgeService_UploadDocument_FullMethodName)
	server, err := zrpc.NewServer(config.RpcServerConf, func(g *grpc.Server) {
		knowledgepb.RegisterKnowledgeServiceServer(g, knowledgeserver.NewGRPCServerWithSearch(service, retrieval, manager, time.Duration(config.Timeout)*time.Millisecond))
	})
	if err != nil {
		log.Fatalf("%s create rpc server: %v", SERVICE_NAME, err)
	}
	defer server.Stop()
	workerCtx, cancelWorker := context.WithCancel(context.Background())
	defer cancelWorker()
	go func() {
		if err := worker.Run(workerCtx, config.Outbox.PollInterval); err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("%s knowledge outbox worker stopped", SERVICE_NAME)
		}
	}()
	go func() {
		if err := ingestConsumer.Run(workerCtx); err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("%s knowledge ingest consumer stopped", SERVICE_NAME)
		}
	}()
	go func() {
		if err := embedConsumer.Run(workerCtx); err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("%s knowledge embed consumer stopped", SERVICE_NAME)
		}
	}()
	server.Start()
}

func buildEmbeddingProvider(config embeddingConfig, runtimeConfig platformconfig.Config) (knowledge.EmbeddingProvider, error) {
	switch strings.ToLower(strings.TrimSpace(config.Provider)) {
	case "dashscope":
		if strings.TrimSpace(runtimeConfig.DashScopeAPIKey) == "" {
			return nil, errors.New("dashscope api key is required")
		}
		client := &http.Client{Timeout: config.Timeout}
		return knowledge.NewDashScopeEmbeddingProvider(knowledge.DashScopeConfig{
			Endpoint: "https://dashscope.aliyuncs.com", APIKey: runtimeConfig.DashScopeAPIKey,
			Model: config.Model, Dimension: config.Dimension, HTTPClient: client,
		})
	default:
		return nil, errors.New("unsupported embedding provider")
	}
}

func buildVectorStore(ctx context.Context, config vectorStoreConfig, runtimeConfig platformconfig.Config, dimension int) (knowledge.VectorStore, error) {
	switch strings.ToLower(strings.TrimSpace(config.Backend)) {
	case "milvus":
		client := &http.Client{Timeout: config.Timeout}
		address := runtimeConfig.MilvusAddress
		if !strings.HasPrefix(address, "http://") && !strings.HasPrefix(address, "https://") {
			address = "http://" + address
		}
		return knowledge.NewMilvusRESTVectorStore(knowledge.MilvusConfig{Address: address, Collection: config.Collection, Dimension: dimension}, client)
	default:
		return nil, errors.New("unsupported vector store backend")
	}
}

func knowledgeDSN(dsn string) (string, error) {
	config, err := mysql.ParseDSN(dsn)
	if err != nil {
		return "", fmt.Errorf("parse mysql dsn: %w", err)
	}
	if config.DBName == "" {
		return "", errors.New("missing mysql database")
	}
	config.DBName = "knowledge_db"
	return config.FormatDSN(), nil
}
