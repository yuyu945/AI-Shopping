package order

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestTradeSchemaOwnsCartAndOrderSnapshots(t *testing.T) {
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() did not return the test source path")
	}
	schemaPath := filepath.Join(filepath.Dir(sourceFile), "..", "..", "..", "..", "deploy", "mysql", "init", "03-trade-schema.sql")
	schema, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatalf("read trade schema: %v", err)
	}

	required := []string{
		"CREATE TABLE carts",
		"CREATE TABLE cart_items",
		"CREATE TABLE orders",
		"CREATE TABLE order_items",
		"DECIMAL(12,2)",
		"UNIQUE KEY uq_orders_user_request (user_id, request_id)",
		"CONSTRAINT fk_cart_items_cart FOREIGN KEY (cart_id) REFERENCES carts(id)",
		"CONSTRAINT fk_order_items_order FOREIGN KEY (order_id) REFERENCES orders(id)",
	}
	for _, value := range required {
		if !strings.Contains(string(schema), value) {
			t.Fatalf("trade schema %s must contain %q", schemaPath, value)
		}
	}
	if strings.Contains(string(schema), "REFERENCES user_db.") || strings.Contains(string(schema), "REFERENCES catalog_db.") {
		t.Fatalf("trade schema %s must not define cross-service foreign keys", schemaPath)
	}
}
