package main

import (
	"strings"
	"testing"
	"time"

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
	if config.Embedding.Model != "text-embedding-3-small" || config.Embedding.Dimension != 1536 || config.Embedding.Timeout != 5*time.Second {
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
