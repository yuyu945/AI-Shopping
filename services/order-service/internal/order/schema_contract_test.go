package order

import (
	"os"
	"path/filepath"
	"regexp"
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
	migrationPath := filepath.Join(filepath.Dir(sourceFile), "..", "..", "..", "..", "deploy", "mysql", "migrations", "20260822_m2_1_trade_schema.sql")
	migration, err := os.ReadFile(migrationPath)
	if err != nil {
		t.Fatalf("read trade migration: %v", err)
	}
	candidateMigrationPath := filepath.Join(filepath.Dir(sourceFile), "..", "..", "..", "..", "deploy", "mysql", "migrations", "20260822_m2_1_trade_z_order_promotion_candidates.sql")
	candidateMigration, err := os.ReadFile(candidateMigrationPath)
	if err != nil {
		t.Fatalf("read candidate promotion migration: %v", err)
	}
	paymentMigrationPath := filepath.Join(filepath.Dir(sourceFile), "..", "..", "..", "..", "deploy", "mysql", "migrations", "20260822_m2_2_payment_reservation.sql")
	paymentMigration, err := os.ReadFile(paymentMigrationPath)
	if err != nil {
		t.Fatalf("read payment reservation migration: %v", err)
	}

	schemaText := string(schema)
	migrationText := string(migration)
	candidateMigrationText := string(candidateMigration)
	required := []string{
		"CREATE TABLE carts",
		"CREATE TABLE cart_items",
		"CREATE TABLE orders",
		"CREATE TABLE order_items",
		"UNIQUE KEY uq_orders_user_request (user_id, request_id)",
		"CONSTRAINT fk_cart_items_cart FOREIGN KEY (cart_id) REFERENCES carts(id)",
		"CONSTRAINT fk_order_items_order FOREIGN KEY (order_id) REFERENCES orders(id)",
	}
	for _, value := range required {
		if !strings.Contains(schemaText, value) {
			t.Fatalf("trade schema %s must contain %q", schemaPath, value)
		}
	}
	for _, column := range []string{"total_amount", "paid_amount", "unit_price", "discount_amount", "item_amount"} {
		pattern := regexp.MustCompile(`(?m)\b` + column + `\s+DECIMAL\(12,2\)`)
		if !pattern.MatchString(schemaText) {
			t.Fatalf("trade schema %s must define %s as DECIMAL(12,2)", schemaPath, column)
		}
	}
	for name, source := range map[string]string{"init schema": schemaText, "candidate migration": candidateMigrationText} {
		if !strings.Contains(source, "candidate_promotions_snapshot JSON NOT NULL") {
			t.Fatalf("%s must persist candidate promotion snapshots", name)
		}
	}
	if !strings.Contains(candidateMigrationText, "JSON_ARRAY()") {
		t.Fatalf("candidate migration must backfill existing rows with an empty JSON array")
	}
	if count := strings.Count(schemaText, "DECIMAL(12,2)"); count != 7 {
		t.Fatalf("trade schema %s must define five order amounts plus wallet balance and ledger amount as DECIMAL(12,2), got %d", schemaPath, count)
	}
	foreignKeys := regexp.MustCompile(`(?m)CONSTRAINT\s+\w+\s+FOREIGN KEY\s+\([^)]*\)\s+REFERENCES\s+\w+\([^)]*\)`).FindAllString(schemaText, -1)
	if len(foreignKeys) != 4 {
		t.Fatalf("trade schema %s may only define same-schema cart, order, and review foreign keys: %v", schemaPath, foreignKeys)
	}
	if strings.Contains(schemaText, "REFERENCES user_db.") || strings.Contains(schemaText, "REFERENCES catalog_db.") {
		t.Fatalf("trade schema %s must not define cross-service foreign keys", schemaPath)
	}
	for name, source := range map[string]string{"M2.1 migration": migrationText, "candidate migration": candidateMigrationText} {
		for _, forbidden := range []string{"inventory_reservations", "payment_attempt_id", "reservation_id"} {
			if strings.Contains(source, forbidden) {
				t.Fatalf("%s must not contain M2.2 field %q", name, forbidden)
			}
		}
	}
	for _, value := range []string{
		"payment_attempt_id CHAR(36) NULL",
		"reservation_id CHAR(36) NULL",
		"payment_started_at DATETIME(3) NULL",
		"UNIQUE KEY uq_orders_payment_attempt (payment_attempt_id)",
		"UNIQUE KEY uq_orders_reservation (reservation_id)",
		"KEY idx_orders_status_payment_started (status, payment_started_at, id)",
		"payment_attempt_history (",
		"UNIQUE KEY uq_payment_attempt_history_attempt (payment_attempt_id)",
	} {
		if !strings.Contains(schemaText, value) || !strings.Contains(string(paymentMigration), value) {
			t.Fatalf("M2.2 payment persistence must contain %q in both init schema and migration", value)
		}
	}
	for _, value := range []string{
		"wallet_accounts (",
		"wallet_ledger (",
		"outbox_events (",
		"UNIQUE KEY uq_wallet_ledger_business (biz_type, biz_id, direction)",
		"UNIQUE KEY uq_outbox_event_id (event_id)",
		"UNIQUE KEY uq_outbox_aggregate_event (aggregate_type, aggregate_id, event_type)",
		"locked_at DATETIME(3) NULL",
		"lease_until DATETIME(3) NULL",
		"KEY idx_outbox_processing_lease (status, lease_until, id)",
	} {
		if !strings.Contains(schemaText, value) || !strings.Contains(string(paymentMigration), value) {
			t.Fatalf("M2.2 payment settlement persistence must contain %q in both init schema and migration", value)
		}
	}
	for _, forbidden := range []string{"catalog_db", "inventory_reservations", "USE catalog_db"} {
		if strings.Contains(string(paymentMigration), forbidden) {
			t.Fatalf("trade payment migration must not contain catalog schema DDL marker %q", forbidden)
		}
	}
	for name, source := range map[string]string{"init schema": schemaText, "M2.1 migration": migrationText} {
		for _, check := range []string{
			"CONSTRAINT chk_cart_items_quantity_positive CHECK (quantity > 0)",
			"CONSTRAINT chk_order_items_quantity_positive CHECK (quantity > 0)",
		} {
			if !strings.Contains(source, check) {
				t.Fatalf("%s must contain %q", name, check)
			}
		}
	}
}

func TestReviewSchemaContract(t *testing.T) {
	root := repositoryRoot(t)
	initSQL := readFile(t, filepath.Join(root, "deploy", "mysql", "init", "03-trade-schema.sql"))
	migrationSQL := readFile(t, filepath.Join(root, "deploy", "mysql", "migrations", "20260824_m5_2a_reviews.sql"))

	for name, sql := range map[string]string{
		"init schema": initSQL,
		"migration":   migrationSQL,
	} {
		for _, want := range []string{
			"CREATE TABLE",
			"reviews",
			"review_no VARCHAR(64) NOT NULL",
			"UNIQUE KEY uq_reviews_order_item (order_item_id)",
			"CONSTRAINT chk_reviews_rating_range CHECK (rating BETWEEN 1 AND 5)",
			"CONSTRAINT fk_reviews_order FOREIGN KEY (order_id) REFERENCES orders(id)",
			"CONSTRAINT fk_reviews_order_item FOREIGN KEY (order_item_id) REFERENCES order_items(id)",
		} {
			if !strings.Contains(sql, want) {
				t.Fatalf("%s review schema missing %q", name, want)
			}
		}
	}
}

func TestOrderProtoReviewContract(t *testing.T) {
	root := repositoryRoot(t)
	proto := readFile(t, filepath.Join(root, "api", "order", "order.proto"))

	for _, want := range []string{
		"rpc SubmitReview(SubmitReviewRequest) returns (ReviewResponse);",
		"message SubmitReviewRequest",
		"message ReviewResponse",
		"message Review",
		"string review_no = 1;",
		"uint32 rating = 5;",
	} {
		if !strings.Contains(proto, want) {
			t.Fatalf("order proto missing %q", want)
		}
	}
}

func TestReviewAnalyticsSchemaContract(t *testing.T) {
	root := repositoryRoot(t)
	initSQL := readFile(t, filepath.Join(root, "deploy", "mysql", "init", "03-trade-schema.sql"))
	migrationSQL := readFile(t, filepath.Join(root, "deploy", "mysql", "migrations", "20260824_m5_2b_review_analytics.sql"))

	for name, sql := range map[string]string{
		"init schema": initSQL,
		"migration":   migrationSQL,
	} {
		for _, want := range []string{
			"CREATE TABLE",
			"review_event_consumptions",
			"PRIMARY KEY (event_id, consumer_group)",
			"review_event_records",
			"UNIQUE KEY uq_review_event_records_event (event_id)",
			"UNIQUE KEY uq_review_event_records_review (review_no)",
			"product_review_stats",
			"rating_avg DECIMAL(4,2) NOT NULL DEFAULT 0.00",
			"PRIMARY KEY (product_id)",
		} {
			if !strings.Contains(sql, want) {
				t.Fatalf("%s review analytics schema missing %q", name, want)
			}
		}
	}
}

func repositoryRoot(t *testing.T) string {
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
			t.Fatal("repository root with go.mod not found")
		}
		dir = parent
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(content)
}
