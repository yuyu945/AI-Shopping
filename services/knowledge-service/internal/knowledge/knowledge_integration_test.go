//go:build integration

package knowledge

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/minio/minio-go/v7"
	"github.com/segmentio/kafka-go"
)

const knowledgeIntegrationTimeout = 10 * time.Second
const knowledgeIntegrationGuardTable = "knowledge_integration_guards"

func TestKnowledgeM32DependencyIntegration(t *testing.T) {
	config, run, err := knowledgeIntegrationConfig(os.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	if !run {
		t.Skip("set AI_SHOPPING_INTEGRATION=1 and AI_SHOPPING_INTEGRATION_ISOLATED=m32knowledge to run isolated knowledge integration tests")
	}

	db, err := sql.Open("mysql", config.mysqlDSN)
	if err != nil {
		t.Fatalf("open knowledge database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := knowledgeIntegrationContext(t)
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping knowledge database: %v", err)
	}
	assertKnowledgeIntegrationDatabase(t, db, config.runID)
	assertKnowledgeIntegrationMinIO(t, config)
	assertKnowledgeIntegrationKafka(t, config.kafkaBrokers)
	assertKnowledgeIntegrationMilvusREST(t, config.milvusAddress)
	assertKnowledgeIntegrationDashScope(t, config.dashScopeAPIKey)
}

func assertKnowledgeIntegrationDatabase(t *testing.T, db *sql.DB, runID string) {
	t.Helper()
	ctx := knowledgeIntegrationContext(t)
	var databaseName string
	if err := db.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&databaseName); err != nil {
		t.Fatalf("read current database: %v", err)
	}
	if databaseName != "knowledge_db" {
		t.Fatalf("current database = %q, want knowledge_db", databaseName)
	}
	var guardCount int
	query := "SELECT COUNT(*) FROM " + knowledgeIntegrationGuardTable + " WHERE run_id = ?"
	if err := db.QueryRowContext(ctx, query, runID).Scan(&guardCount); err != nil {
		t.Fatalf("verify knowledge integration guard: %v", err)
	}
	if guardCount != 1 {
		t.Fatalf("knowledge integration guard count = %d, want 1", guardCount)
	}
}

func assertKnowledgeIntegrationMinIO(t *testing.T, config knowledgeIntegrationSettings) {
	t.Helper()
	storage, err := NewMinIOStorage(config.minIOEndpoint, config.minIOAccessKey, config.minIOSecretKey, false)
	if err != nil {
		t.Fatalf("create MinIO storage: %v", err)
	}
	ctx := knowledgeIntegrationContext(t)
	exists, err := storage.client.BucketExists(ctx, config.minIOBucket)
	if err != nil {
		t.Fatalf("check MinIO bucket: %v", err)
	}
	if !exists {
		if err := storage.client.MakeBucket(ctx, config.minIOBucket, minio.MakeBucketOptions{}); err != nil {
			t.Fatalf("create MinIO bucket: %v", err)
		}
	}
	key := fmt.Sprintf("integration/%s/m3.2-smoke.txt", config.runID)
	content := []byte("DashScope and Milvus integration smoke")
	if err := storage.PutObject(ctx, config.minIOBucket, key, content, "text/plain"); err != nil {
		t.Fatalf("put MinIO object: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), knowledgeIntegrationTimeout)
		defer cancel()
		if err := storage.DeleteObject(cleanupCtx, config.minIOBucket, key); err != nil {
			t.Errorf("clean up MinIO object: %v", err)
		}
	})
	got, err := storage.GetObject(ctx, config.minIOBucket, key)
	if err != nil {
		t.Fatalf("get MinIO object: %v", err)
	}
	if string(got) != string(content) {
		t.Fatalf("MinIO object = %q, want %q", got, content)
	}
}

func assertKnowledgeIntegrationKafka(t *testing.T, brokers []string) {
	t.Helper()
	dialer := kafka.Dialer{Timeout: knowledgeIntegrationTimeout}
	conn, err := dialer.DialContext(knowledgeIntegrationContext(t), "tcp", brokers[0])
	if err != nil {
		t.Fatalf("dial Kafka: %v", err)
	}
	defer conn.Close()
	partitions, err := conn.ReadPartitions(documentIngestTopic, chunkEmbedTopic)
	if err != nil {
		t.Fatalf("read Kafka partitions: %v", err)
	}
	seen := make(map[string]bool)
	for _, partition := range partitions {
		seen[partition.Topic] = true
	}
	for _, topic := range []string{documentIngestTopic, chunkEmbedTopic} {
		if !seen[topic] {
			t.Fatalf("Kafka topic %q is not visible", topic)
		}
	}
}

func assertKnowledgeIntegrationMilvusREST(t *testing.T, address string) {
	t.Helper()
	endpoint := strings.TrimRight(address, "/") + "/v2/vectordb/collections/list"
	body, err := json.Marshal(map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequestWithContext(knowledgeIntegrationContext(t), http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("create Milvus request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: knowledgeIntegrationTimeout}).Do(req)
	if err != nil {
		t.Fatalf("call Milvus REST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		t.Fatalf("Milvus REST status = %d, want 2xx", resp.StatusCode)
	}
}

func assertKnowledgeIntegrationDashScope(t *testing.T, apiKey string) {
	t.Helper()
	provider, err := NewDashScopeEmbeddingProvider(DashScopeConfig{
		Endpoint:   "https://dashscope.aliyuncs.com",
		APIKey:     apiKey,
		Model:      "text-embedding-v4",
		Dimension:  1024,
		HTTPClient: &http.Client{Timeout: knowledgeIntegrationTimeout},
	})
	if err != nil {
		t.Fatalf("create DashScope provider: %v", err)
	}
	output, err := provider.EmbedDocuments(knowledgeIntegrationContext(t), EmbeddingInput{Texts: []string{"knowledge integration smoke"}})
	if err != nil {
		t.Fatalf("embed DashScope document: %v", err)
	}
	if len(output.Vectors) != 1 || len(output.Vectors[0]) != 1024 {
		t.Fatalf("DashScope vector shape = %d/%d, want 1/1024", len(output.Vectors), firstVectorLength(output.Vectors))
	}
}

func firstVectorLength(vectors [][]float32) int {
	if len(vectors) == 0 {
		return 0
	}
	return len(vectors[0])
}

func knowledgeIntegrationContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), knowledgeIntegrationTimeout)
	t.Cleanup(cancel)
	return ctx
}
