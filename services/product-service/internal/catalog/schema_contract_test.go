package catalog

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCatalogSchemaCacheInvalidationRetryCountIsUnsigned(t *testing.T) {
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() did not return the test source path")
	}
	schemaPath := filepath.Join(filepath.Dir(sourceFile), "..", "..", "..", "..", "deploy", "mysql", "init", "02-catalog-schema.sql")
	schema, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatalf("read catalog schema: %v", err)
	}
	if !strings.Contains(string(schema), "retry_count INT UNSIGNED NOT NULL DEFAULT 0") {
		t.Fatalf("catalog schema %s must declare retry_count as INT UNSIGNED NOT NULL DEFAULT 0", schemaPath)
	}
}
