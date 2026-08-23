package knowledge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestKnowledgeUploadSchemaContract(t *testing.T) {
	schema := readFile(t, filepath.Join(repoRoot(t), "deploy/mysql/init/05-knowledge-schema.sql"))

	assertContains(t, schema, "CREATE TABLE knowledge_documents")
	assertContains(t, schema, "CONSTRAINT chk_knowledge_document_type CHECK (doc_type IN ('DETAIL','SPEC','FAQ','AFTER_SALE'))")
	assertContains(t, schema, "UNIQUE KEY uq_knowledge_document_source (product_id, doc_type, source_hash)")
	assertContains(t, schema, "CREATE TABLE outbox_events")
	assertContains(t, schema, "KEY idx_knowledge_outbox_status_retry (status, next_retry_at, id)")
	assertContains(t, schema, "CREATE TABLE event_consumptions")
	assertContains(t, schema, "CONSTRAINT chk_knowledge_event_consumption_status CHECK (status IN ('PROCESSING','SUCCEEDED','FAILED'))")
	assertContains(t, schema, "CREATE TABLE embedding_tasks")
	assertContains(t, schema, "is_current_ready TINYINT(1) NOT NULL DEFAULT 0")
	assertContains(t, schema, "ready_at DATETIME(3) NULL")
	assertContains(t, schema, "CREATE TABLE knowledge_chunks")
	assertContains(t, schema, "UNIQUE KEY uq_knowledge_chunk_index (document_id, chunk_index)")
	assertContains(t, schema, "UNIQUE KEY uq_knowledge_chunk_content (document_id, content_hash)")
	assertContains(t, schema, "KEY idx_knowledge_chunk_visible (product_id, doc_type, version, status, chunk_index)")
	assertContains(t, schema, "CONSTRAINT chk_knowledge_chunk_status CHECK (status IN ('PENDING_EMBEDDING','EMBEDDED','FAILED'))")
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("repository root not found")
		}
		dir = parent
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func assertContains(t *testing.T, content, want string) {
	t.Helper()
	if !strings.Contains(content, want) {
		t.Fatalf("schema does not contain %q", want)
	}
}
