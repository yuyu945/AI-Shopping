//go:build integration

package catalog

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
)

// TestInventoryReservationIntegration exercises the catalog-local transaction
// only when the existing isolated M1.2 integration guard is explicitly enabled.
func TestInventoryReservationIntegration(t *testing.T) {
	config, run, err := cacheInvalidationIntegrationConfig(os.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	if !run {
		t.Skip("set AI_SHOPPING_INTEGRATION=1 and AI_SHOPPING_INTEGRATION_ISOLATED=m12cacheverify to run isolated integration tests")
	}
	db, err := sql.Open("mysql", config.mysqlDSN)
	if err != nil {
		t.Fatalf("open catalog database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), integrationTimeout)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping catalog database: %v", err)
	}
	assertDatabaseIntegrationGuard(t, db, config.runID)

	items := []ReservationItem{{SKUID: 1101, Quantity: 2}, {SKUID: 1102, Quantity: 1}}
	before := loadIntegrationInventory(t, db, items)
	var taskWatermark uint64
	if err := db.QueryRowContext(ctx, "SELECT COALESCE(MAX(id), 0) FROM cache_invalidation_tasks").Scan(&taskWatermark); err != nil {
		t.Fatalf("load cache task watermark: %v", err)
	}
	reservationID := uuid.NewString()
	now := time.Now().UTC().Truncate(time.Millisecond)
	service, err := NewReservationService(NewReservationRepository(db), nil, func() time.Time { return now }, 100*time.Millisecond, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), integrationTimeout)
		defer cleanupCancel()
		if _, err := db.ExecContext(cleanupCtx, "DELETE FROM cache_invalidation_tasks WHERE id > ? AND cache_key IN (?, ?, ?)", taskWatermark, ProductCacheKey(1001, nil), ProductCacheKey(1001, uint64Ptr(1101)), ProductCacheKey(1001, uint64Ptr(1102))); err != nil {
			t.Errorf("clean up reservation cache tasks: %v", err)
		}
		if _, err := db.ExecContext(cleanupCtx, "DELETE FROM inventory_reservations WHERE reservation_id = ?", reservationID); err != nil {
			t.Errorf("clean up reservation rows: %v", err)
		}
	})

	reserved, err := service.ReserveStock(ctx, ReserveStockInput{ReservationID: reservationID, OrderNo: "integration-order", PaymentAttemptID: uuid.NewString(), Items: items, ExpiresAt: now.Add(time.Minute)})
	if err != nil || reserved.Status != ReservationReserved || len(reserved.Items) != len(items) {
		t.Fatalf("ReserveStock()=%#v err=%v", reserved, err)
	}
	afterReserve := loadIntegrationInventory(t, db, items)
	for _, item := range items {
		if afterReserve[item.SKUID] != before[item.SKUID]-uint64(item.Quantity) {
			t.Fatalf("sku %d available=%d want %d", item.SKUID, afterReserve[item.SKUID], before[item.SKUID]-uint64(item.Quantity))
		}
	}
	var taskCount int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM cache_invalidation_tasks WHERE cache_key IN (?, ?, ?)", ProductCacheKey(1001, nil), ProductCacheKey(1001, uint64Ptr(1101)), ProductCacheKey(1001, uint64Ptr(1102))).Scan(&taskCount); err != nil || taskCount != 3 {
		t.Fatalf("reservation cache tasks=%d err=%v", taskCount, err)
	}

	released, err := service.ReleaseReservation(ctx, reservationID)
	if err != nil || released.Status != ReservationReleased {
		t.Fatalf("ReleaseReservation()=%#v err=%v", released, err)
	}
	afterRelease := loadIntegrationInventory(t, db, items)
	for _, item := range items {
		if afterRelease[item.SKUID] != before[item.SKUID] {
			t.Fatalf("sku %d available=%d want restored %d", item.SKUID, afterRelease[item.SKUID], before[item.SKUID])
		}
	}
}

func loadIntegrationInventory(t *testing.T, db *sql.DB, items []ReservationItem) map[uint64]uint64 {
	t.Helper()
	result := make(map[uint64]uint64, len(items))
	for _, item := range items {
		var quantity uint64
		if err := db.QueryRowContext(testContext(t), "SELECT available_qty FROM inventory WHERE sku_id = ?", item.SKUID).Scan(&quantity); err != nil {
			t.Fatalf("load inventory for SKU %d: %v", item.SKUID, err)
		}
		result[item.SKUID] = quantity
	}
	return result
}
