package main

import (
	"context"
	"strings"
	"testing"
	"time"

	platformconfig "github.com/yuyu945/AI-Shopping/internal/platform/config"
	"github.com/zeromicro/go-zero/core/conf"
)

func TestKnowledgeServiceConfigRequiresUploadLimits(t *testing.T) {
	config := knowledgeServiceConfig{}
	if err := config.validate(); err == nil {
		t.Fatal("validate() error = nil, want missing upload config error")
	}
}

func TestKnowledgeServiceConfigLoadsDefaults(t *testing.T) {
	var config knowledgeServiceConfig
	if err := conf.Load("../etc/knowledge-service.yaml", &config); err != nil {
		t.Fatal(err)
	}
	if err := config.validate(); err != nil {
		t.Fatalf("validate() error = %v", err)
	}
	if config.Upload.Bucket != "knowledge" || config.Upload.MaxFileBytes != 2*1024*1024 {
		t.Fatalf("upload config = %#v", config.Upload)
	}
	if config.Outbox.PollInterval != time.Second || config.Outbox.MaxAttempts != 5 {
		t.Fatalf("outbox config = %#v", config.Outbox)
	}
	if config.Embedding.Provider != "dashscope" || config.Embedding.Model != "text-embedding-v4" || config.Embedding.Dimension != 1024 || config.Embedding.Timeout != 5*time.Second {
		t.Fatalf("embedding config = %#v", config.Embedding)
	}
	if config.VectorStore.Backend != "milvus" || config.VectorStore.Collection != "knowledge_chunks" || config.VectorStore.Timeout != 5*time.Second {
		t.Fatalf("vector store config = %#v", config.VectorStore)
	}
}

func TestKnowledgeDSNUsesKnowledgeDatabase(t *testing.T) {
	got, err := knowledgeDSN("app:secret@tcp(localhost:3306)/user_db?parseTime=true")
	if err != nil || !strings.Contains(got, "/knowledge_db?") {
		t.Fatalf("dsn=%q err=%v", got, err)
	}
}

func TestBuildEmbeddingProviderRequiresDashScopeKey(t *testing.T) {
	_, err := buildEmbeddingProvider(embeddingConfig{Provider: "dashscope", Model: "text-embedding-v4", Dimension: 1024, Timeout: time.Second}, platformconfig.Config{})
	if err == nil {
		t.Fatal("buildEmbeddingProvider() error = nil, want missing key")
	}
}

func TestBuildEmbeddingProviderCreatesDashScopeProvider(t *testing.T) {
	provider, err := buildEmbeddingProvider(embeddingConfig{Provider: "dashscope", Model: "text-embedding-v4", Dimension: 1024, Timeout: time.Second}, platformconfig.Config{DashScopeAPIKey: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	if provider == nil {
		t.Fatal("provider = nil")
	}
}

func TestBuildVectorStoreCreatesMilvusRESTStore(t *testing.T) {
	store, err := buildVectorStore(context.Background(), vectorStoreConfig{Backend: "milvus", Collection: "knowledge_chunks", Timeout: time.Second}, platformconfig.Config{MilvusAddress: "http://localhost:19530"}, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if store == nil {
		t.Fatal("store = nil")
	}
}
