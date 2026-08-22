//go:build integration

package order

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

const orderSnapshotIntegrationGuardTable = "order_snapshot_integration_guards"

type orderSnapshotIntegrationFixture struct {
	t                *testing.T
	ctx              context.Context
	tradeDB          *sql.DB
	userDB           *sql.DB
	catalogDB        *sql.DB
	tradeRepository  *MySQLRepository
	addresses        mysqlIntegrationAddressReader
	products         mysqlIntegrationProductReader
	runID            string
	ownerID          uint64
	foreignUserID    uint64
	ownerAddressID   uint64
	foreignAddressID uint64
	productID        uint64
	categoryID       uint64
	skuID            uint64
	secondarySKUID   uint64
	requestID        string
	productTitle     string
	salePrice        string
}

func newOrderSnapshotIntegrationFixture(t *testing.T, parent context.Context, settings orderSnapshotIntegrationSettings) *orderSnapshotIntegrationFixture {
	t.Helper()
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	t.Cleanup(cancel)
	userDSN, err := integrationDSNForDatabase(settings.mysqlDSN, "user_db")
	if err != nil {
		t.Fatal(err)
	}
	catalogDSN, err := integrationDSNForDatabase(settings.mysqlDSN, "catalog_db")
	if err != nil {
		t.Fatal(err)
	}
	tradeDB := openIntegrationDatabase(t, settings.mysqlDSN, "trade_db")
	userDB := openIntegrationDatabase(t, userDSN, "user_db")
	catalogDB := openIntegrationDatabase(t, catalogDSN, "catalog_db")

	var guards int
	if err := tradeDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+orderSnapshotIntegrationGuardTable+" WHERE run_id = ?", settings.runID).Scan(&guards); err != nil {
		t.Fatalf("verify order integration guard: %v", err)
	}
	if guards != 1 {
		t.Fatalf("order integration guard count = %d, want 1", guards)
	}

	fixture := &orderSnapshotIntegrationFixture{
		t:               t,
		ctx:             ctx,
		tradeDB:         tradeDB,
		userDB:          userDB,
		catalogDB:       catalogDB,
		tradeRepository: NewMySQLRepository(tradeDB),
		addresses:       mysqlIntegrationAddressReader{db: userDB},
		products:        mysqlIntegrationProductReader{db: catalogDB},
		runID:           settings.runID,
		requestID:       "order-integration-" + settings.runID,
		productTitle:    "Order integration product " + settings.runID,
		salePrice:       "12.34",
	}
	fixture.ownerID = insertIntegrationUser(t, ctx, userDB, "owner-"+settings.runID+"@integration.invalid")
	fixture.foreignUserID = insertIntegrationUser(t, ctx, userDB, "foreign-"+settings.runID+"@integration.invalid")
	fixture.ownerAddressID = insertIntegrationAddress(t, ctx, userDB, fixture.ownerID)
	fixture.foreignAddressID = insertIntegrationAddress(t, ctx, userDB, fixture.foreignUserID)
	fixture.categoryID = insertIntegrationCategory(t, ctx, catalogDB, "Order integration "+settings.runID)
	fixture.productID = insertIntegrationProduct(t, ctx, catalogDB, fixture.categoryID, fixture.productTitle)
	fixture.skuID = insertIntegrationSKU(t, ctx, catalogDB, fixture.productID, "order-integration-"+settings.runID+"-primary", fixture.salePrice)
	fixture.secondarySKUID = insertIntegrationSKU(t, ctx, catalogDB, fixture.productID, "order-integration-"+settings.runID+"-secondary", "5.00")
	return fixture
}

func openIntegrationDatabase(t *testing.T, dsn, expectedDatabase string) *sql.DB {
	t.Helper()
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatalf("open %s: %v", expectedDatabase, err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping %s: %v", expectedDatabase, err)
	}
	var actual string
	if err := db.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&actual); err != nil {
		t.Fatalf("read %s database name: %v", expectedDatabase, err)
	}
	if actual != expectedDatabase {
		t.Fatalf("database = %q, want %q", actual, expectedDatabase)
	}
	return db
}

func insertIntegrationUser(t *testing.T, ctx context.Context, db *sql.DB, email string) uint64 {
	t.Helper()
	result, err := db.ExecContext(ctx, "INSERT INTO users (email, password_hash, status) VALUES (?, ?, 'ACTIVE')", email, "integration-password-hash")
	if err != nil {
		t.Fatalf("insert integration user: %v", err)
	}
	return integrationLastInsertID(t, result, "user")
}

func insertIntegrationAddress(t *testing.T, ctx context.Context, db *sql.DB, userID uint64) uint64 {
	t.Helper()
	result, err := db.ExecContext(ctx, "INSERT INTO user_addresses (user_id, receiver_name, receiver_phone, province, city, district, detail, is_default) VALUES (?, ?, ?, ?, ?, ?, ?, TRUE)", userID, "Integration User", "10000000000", "Test Province", "Test City", "Test District", "Test Detail")
	if err != nil {
		t.Fatalf("insert integration address: %v", err)
	}
	return integrationLastInsertID(t, result, "address")
}

func insertIntegrationCategory(t *testing.T, ctx context.Context, db *sql.DB, name string) uint64 {
	t.Helper()
	result, err := db.ExecContext(ctx, "INSERT INTO categories (name, status) VALUES (?, 'ACTIVE')", name)
	if err != nil {
		t.Fatalf("insert integration category: %v", err)
	}
	return integrationLastInsertID(t, result, "category")
}

func insertIntegrationProduct(t *testing.T, ctx context.Context, db *sql.DB, categoryID uint64, title string) uint64 {
	t.Helper()
	result, err := db.ExecContext(ctx, "INSERT INTO products (category_id, title, status) VALUES (?, ?, 'ACTIVE')", categoryID, title)
	if err != nil {
		t.Fatalf("insert integration product: %v", err)
	}
	return integrationLastInsertID(t, result, "product")
}

func insertIntegrationSKU(t *testing.T, ctx context.Context, db *sql.DB, productID uint64, code, price string) uint64 {
	t.Helper()
	result, err := db.ExecContext(ctx, "INSERT INTO product_skus (product_id, sku_code, spec_json, sale_price, status) VALUES (?, ?, ?, ?, 'ACTIVE')", productID, code, `{"size":"integration"}`, price)
	if err != nil {
		t.Fatalf("insert integration sku: %v", err)
	}
	return integrationLastInsertID(t, result, "sku")
}

func integrationLastInsertID(t *testing.T, result sql.Result, resource string) uint64 {
	t.Helper()
	id, err := result.LastInsertId()
	if err != nil || id <= 0 {
		t.Fatalf("read integration %s id: %v", resource, err)
	}
	return uint64(id)
}

func (f *orderSnapshotIntegrationFixture) cleanup() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, statement := range []struct {
		db  *sql.DB
		sql string
	}{
		{f.tradeDB, "DELETE item FROM order_items item JOIN orders o ON o.id = item.order_id WHERE o.user_id IN (?, ?)"},
		{f.tradeDB, "DELETE FROM orders WHERE user_id IN (?, ?)"},
		{f.tradeDB, "DELETE item FROM cart_items item JOIN carts c ON c.id = item.cart_id WHERE c.user_id IN (?, ?)"},
		{f.tradeDB, "DELETE FROM carts WHERE user_id IN (?, ?)"},
	} {
		if _, err := statement.db.ExecContext(ctx, statement.sql, f.ownerID, f.foreignUserID); err != nil {
			f.reportCleanupError("trade fixture", err)
		}
	}
	for _, statement := range []string{
		"DELETE FROM inventory WHERE sku_id IN (?, ?)",
		"DELETE FROM product_skus WHERE id IN (?, ?)",
		"DELETE FROM promotion_rules WHERE product_id = ?",
		"DELETE FROM products WHERE id = ?",
		"DELETE FROM categories WHERE id = ?",
	} {
		args := []any{f.skuID, f.secondarySKUID}
		if strings.Contains(statement, "product_id") || strings.Contains(statement, "products WHERE") || strings.Contains(statement, "categories WHERE") {
			args = []any{f.productID}
		}
		if strings.Contains(statement, "categories WHERE") {
			args = []any{f.categoryID}
		}
		if _, err := f.catalogDB.ExecContext(ctx, statement, args...); err != nil {
			f.reportCleanupError("catalog fixture", err)
		}
	}
	if _, err := f.userDB.ExecContext(ctx, "DELETE FROM user_addresses WHERE user_id IN (?, ?)", f.ownerID, f.foreignUserID); err != nil {
		f.reportCleanupError("user addresses", err)
	}
	if _, err := f.userDB.ExecContext(ctx, "DELETE FROM users WHERE id IN (?, ?)", f.ownerID, f.foreignUserID); err != nil {
		f.reportCleanupError("users", err)
	}
	if _, err := f.tradeDB.ExecContext(ctx, "DELETE FROM "+orderSnapshotIntegrationGuardTable+" WHERE run_id = ?", f.runID); err != nil {
		f.reportCleanupError("integration guard", err)
	}
}

func (f *orderSnapshotIntegrationFixture) reportCleanupError(resource string, err error) {
	if err != nil {
		f.t.Errorf("clean up order snapshot integration %s: %v", resource, err)
	}
}

type mysqlIntegrationAddressReader struct{ db *sql.DB }

func (r mysqlIntegrationAddressReader) GetAddress(ctx context.Context, userID, addressID uint64) (AddressSnapshot, error) {
	var result AddressSnapshot
	err := r.db.QueryRowContext(ctx, "SELECT id, receiver_name, receiver_phone, province, city, district, detail FROM user_addresses WHERE id = ? AND user_id = ?", addressID, userID).Scan(&result.AddressID, &result.ReceiverName, &result.ReceiverPhone, &result.Province, &result.City, &result.District, &result.Detail)
	if errors.Is(err, sql.ErrNoRows) {
		return AddressSnapshot{}, ErrNotFound
	}
	if err != nil {
		return AddressSnapshot{}, fmt.Errorf("read integration address: %w", err)
	}
	return result, nil
}

type mysqlIntegrationProductReader struct{ db *sql.DB }

func (r mysqlIntegrationProductReader) GetProducts(ctx context.Context, skuIDs []uint64) ([]ProductSnapshot, error) {
	result := make([]ProductSnapshot, 0, len(skuIDs))
	for _, skuID := range skuIDs {
		var item ProductSnapshot
		err := r.db.QueryRowContext(ctx, "SELECT ps.product_id, ps.id, p.title, ps.sku_code, ps.spec_json, ps.sale_price FROM product_skus ps JOIN products p ON p.id = ps.product_id WHERE ps.id = ? AND p.status = 'ACTIVE' AND p.deleted_at IS NULL AND ps.status = 'ACTIVE'", skuID).Scan(&item.ProductID, &item.SKUID, &item.ProductTitle, &item.SKUCode, &item.SpecJSON, &item.UnitPrice)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		if err != nil {
			return nil, fmt.Errorf("read integration checkout sku: %w", err)
		}
		item.Saleable = true
		item.Promotions = []PromotionSnapshot{}
		result = append(result, item)
	}
	return result, nil
}
