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

func TestPaymentReservationSchema(t *testing.T) {
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() did not return the test source path")
	}
	root := filepath.Join(filepath.Dir(sourceFile), "..", "..", "..", "..")
	catalogSchemaPath := filepath.Join(root, "deploy", "mysql", "init", "02-catalog-schema.sql")
	tradeSchemaPath := filepath.Join(root, "deploy", "mysql", "init", "03-trade-schema.sql")
	migrationPath := filepath.Join(root, "deploy", "mysql", "migrations", "20260822_m2_2_payment_reservation.sql")

	for path, required := range map[string][]string{
		catalogSchemaPath: {
			"CREATE TABLE inventory_reservations",
			"UNIQUE KEY uq_inventory_reservation_sku (reservation_id, sku_id)",
			"KEY idx_inventory_reservation_status_expiry (status, expires_at, id)",
			"CONSTRAINT fk_inventory_reservation_sku FOREIGN KEY (sku_id) REFERENCES product_skus(id)",
		},
		tradeSchemaPath: {
			"payment_attempt_id CHAR(36) NULL",
			"reservation_id CHAR(36) NULL",
			"payment_started_at DATETIME(3) NULL",
		},
		migrationPath: {
			"USE catalog_db",
			"CREATE TABLE IF NOT EXISTS inventory_reservations",
			"USE trade_db",
			"payment_attempt_id CHAR(36) NULL",
			"reservation_id CHAR(36) NULL",
			"payment_started_at DATETIME(3) NULL",
		},
	} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read schema source %s: %v", path, err)
		}
		for _, value := range required {
			if !strings.Contains(string(content), value) {
				t.Fatalf("schema source %s must contain %q", path, value)
			}
		}
	}
}
