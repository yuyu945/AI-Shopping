//go:build integration

package catalog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"sort"
	"sync"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/redis/go-redis/v9"
)

const integrationTimeout = 5 * time.Second

const cacheInvalidationIntegrationGuardTable = "cache_invalidation_integration_guards"

func TestCacheInvalidationIntegration(t *testing.T) {
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
	redisClient := redis.NewClient(&redis.Options{Addr: config.redisAddr, DB: config.redisDB})
	t.Cleanup(func() { _ = redisClient.Close() })

	pingCtx, cancelPing := context.WithTimeout(context.Background(), integrationTimeout)
	defer cancelPing()
	if err := db.PingContext(pingCtx); err != nil {
		t.Fatalf("ping catalog database: %v", err)
	}
	var databaseName string
	if err := db.QueryRowContext(pingCtx, "SELECT DATABASE()").Scan(&databaseName); err != nil {
		t.Fatalf("read current database: %v", err)
	}
	if databaseName != "catalog_db" {
		t.Fatalf("current database = %q, want catalog_db", databaseName)
	}
	assertDatabaseIntegrationGuard(t, db, config.runID)
	if err := redisClient.Ping(pingCtx).Err(); err != nil {
		t.Fatalf("ping Redis: %v", err)
	}
	assertIsolatedIntegrationState(t, db, redisClient)

	fixture := discoverCatalogFixture(t, db)
	cleanup := integrationFixtureCleanup{
		expectedVersion: fixture.version,
		expectedDetail:  fixture.detailMarkdown,
	}
	cacheKeys := make([]string, 0, len(fixture.skuIDs)+1)
	cacheKeys = append(cacheKeys, ProductCacheKey(fixture.productID, nil))
	for _, skuID := range fixture.skuIDs {
		skuID := skuID
		cacheKeys = append(cacheKeys, ProductCacheKey(fixture.productID, &skuID))
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), integrationTimeout)
		defer cancel()
		for _, taskID := range cleanup.taskIDs {
			if _, err := db.ExecContext(cleanupCtx, "DELETE FROM cache_invalidation_tasks WHERE id = ?", taskID); err != nil {
				t.Errorf("clean up cache invalidation task %d: %v", taskID, err)
			}
		}
		result, err := db.ExecContext(cleanupCtx, "UPDATE products SET detail_markdown = ?, version = ?, updated_at = ? WHERE id = ? AND version = ? AND detail_markdown <=> ?", fixture.detailMarkdown, fixture.version, fixture.updatedAt, fixture.productID, cleanup.expectedVersion, cleanup.expectedDetail)
		if err != nil {
			t.Errorf("restore seeded product: %v", err)
		} else if affected, err := result.RowsAffected(); err != nil || affected != 1 {
			t.Errorf("restore seeded product affected %d rows, want 1", affected)
		}
		if err := redisClient.Del(cleanupCtx, cacheKeys...).Err(); err != nil {
			t.Errorf("clean up Redis keys: %v", err)
		}
	})

	realCache := NewRedisDetailCache(redisClient)
	mutationRepository := NewMutationRepository(db)
	worker := newIntegrationInvalidationWorker(t, db, realCache)

	t.Run("immediate deletion and delayed worker completion", func(t *testing.T) {
		preloadCacheKeys(t, realCache, cacheKeys)
		mutationNow := time.Now().UTC()
		service := newIntegrationMutationService(t, mutationRepository, realCache, mutationNow)

		result, err := service.UpdateProductDetail(testContext(t), fixture.productID, "integration immediate invalidation")
		if err != nil {
			t.Fatalf("UpdateProductDetail() error = %v", err)
		}
		if err := cleanup.recordMutation(result); err != nil {
			t.Fatal(err)
		}
		assertSameCacheKeys(t, result.CacheKeys, cacheKeys)
		assertCacheKeysExist(t, redisClient, cacheKeys, false)
		tasks := loadTasksByID(t, db, result.TaskIDs)
		assertTaskStatuses(t, tasks, CacheInvalidationPending)

		runCtx, cancel := context.WithTimeout(context.Background(), integrationTimeout)
		defer cancel()
		if err := worker.RunOnce(runCtx, mutationNow.Add(time.Second)); err != nil {
			t.Fatalf("RunOnce() error = %v", err)
		}
		assertTaskStatuses(t, loadTasksByID(t, db, taskIDs(tasks)), CacheInvalidationDone)
	})

	t.Run("failed immediate deletion remains durable until worker retry", func(t *testing.T) {
		preloadCacheKeys(t, realCache, cacheKeys)
		mutationNow := time.Now().UTC()
		failingCache := &failFirstDeleteCache{DetailCache: realCache}
		service := newIntegrationMutationService(t, mutationRepository, failingCache, mutationNow)

		result, err := service.UpdateProductDetail(testContext(t), fixture.productID, "integration retry invalidation")
		if err != nil {
			t.Fatalf("UpdateProductDetail() error = %v", err)
		}
		if err := cleanup.recordMutation(result); err != nil {
			t.Fatal(err)
		}
		assertSameCacheKeys(t, result.CacheKeys, cacheKeys)
		if got := failingCache.failedKey(); got != cacheKeys[0] {
			t.Fatalf("failed immediate delete key = %q, want %q", got, cacheKeys[0])
		}
		assertCacheKeyExists(t, redisClient, cacheKeys[0], true)
		assertCacheKeysExist(t, redisClient, cacheKeys[1:], false)
		tasks := loadTasksByID(t, db, result.TaskIDs)
		assertTaskStatuses(t, tasks, CacheInvalidationPending)

		runCtx, cancel := context.WithTimeout(context.Background(), integrationTimeout)
		defer cancel()
		if err := worker.RunOnce(runCtx, mutationNow.Add(time.Second)); err != nil {
			t.Fatalf("RunOnce() error = %v", err)
		}
		assertCacheKeysExist(t, redisClient, cacheKeys, false)
		assertTaskStatuses(t, loadTasksByID(t, db, taskIDs(tasks)), CacheInvalidationDone)
	})
}

func assertDatabaseIntegrationGuard(t *testing.T, db *sql.DB, runID string) {
	t.Helper()
	var guardCount int
	query := "SELECT COUNT(*) FROM " + cacheInvalidationIntegrationGuardTable + " WHERE run_id = ?"
	if err := db.QueryRowContext(testContext(t), query, runID).Scan(&guardCount); err != nil {
		t.Fatalf("verify database integration guard: %v", err)
	}
	if guardCount != 1 {
		t.Fatalf("database integration guard count = %d, want 1 for run ID", guardCount)
	}
}

func assertIsolatedIntegrationState(t *testing.T, db *sql.DB, redisClient *redis.Client) {
	t.Helper()
	ctx := testContext(t)

	var taskCount int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM cache_invalidation_tasks").Scan(&taskCount); err != nil {
		t.Fatalf("count cache invalidation tasks: %v", err)
	}
	if taskCount != 0 {
		t.Fatalf("cache_invalidation_tasks count = %d, want 0 in isolated integration database", taskCount)
	}

	keyCount, err := redisClient.DBSize(ctx).Result()
	if err != nil {
		t.Fatalf("read Redis DB size: %v", err)
	}
	if keyCount != 0 {
		t.Fatalf("Redis DB size = %d, want 0 in isolated integration database", keyCount)
	}
}

type catalogFixture struct {
	productID      uint64
	skuIDs         []uint64
	detailMarkdown sql.NullString
	version        uint64
	updatedAt      time.Time
}

type integrationFixtureCleanup struct {
	taskIDs         []uint64
	expectedVersion uint64
	expectedDetail  sql.NullString
}

func (c *integrationFixtureCleanup) recordMutation(result MutationResult) error {
	c.taskIDs = append(c.taskIDs, result.TaskIDs...)
	c.expectedVersion++
	c.expectedDetail = sql.NullString{String: result.DetailMarkdown, Valid: true}
	if len(result.TaskIDs) != len(result.CacheKeys) {
		return fmt.Errorf("mutation task ID count = %d, want %d", len(result.TaskIDs), len(result.CacheKeys))
	}
	return nil
}

func TestIntegrationFixtureCleanupRecordsRestoreStateBeforeTaskIDValidation(t *testing.T) {
	cleanup := integrationFixtureCleanup{
		expectedVersion: 7,
	}
	result := MutationResult{
		DetailMarkdown: "updated detail",
		CacheKeys:      []string{"cache-one", "cache-two"},
		TaskIDs:        []uint64{101},
	}

	err := cleanup.recordMutation(result)
	if err == nil {
		t.Fatal("recordMutation() error = nil, want task ID mismatch")
	}
	if cleanup.expectedVersion != 8 || !cleanup.expectedDetail.Valid || cleanup.expectedDetail.String != "updated detail" {
		t.Fatalf("cleanup restore state = %#v, want next mutation state", cleanup)
	}
	if len(cleanup.taskIDs) != 1 || cleanup.taskIDs[0] != 101 {
		t.Fatalf("cleanup task IDs = %v, want [101]", cleanup.taskIDs)
	}
}

func discoverCatalogFixture(t *testing.T, db *sql.DB) catalogFixture {
	t.Helper()
	ctx := testContext(t)
	var fixture catalogFixture
	err := db.QueryRowContext(ctx, `
		SELECT p.id, p.detail_markdown, p.version, p.updated_at
		FROM products p
		WHERE p.deleted_at IS NULL
		  AND EXISTS (SELECT 1 FROM product_skus s WHERE s.product_id = p.id)
		ORDER BY p.id
		LIMIT 1`).Scan(&fixture.productID, &fixture.detailMarkdown, &fixture.version, &fixture.updatedAt)
	if err != nil {
		t.Fatalf("discover seeded catalog product: %v", err)
	}

	rows, err := db.QueryContext(ctx, "SELECT id FROM product_skus WHERE product_id = ? ORDER BY id", fixture.productID)
	if err != nil {
		t.Fatalf("load seeded product SKUs: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var skuID uint64
		if err := rows.Scan(&skuID); err != nil {
			t.Fatalf("scan seeded product SKU: %v", err)
		}
		fixture.skuIDs = append(fixture.skuIDs, skuID)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate seeded product SKUs: %v", err)
	}
	if len(fixture.skuIDs) == 0 {
		t.Fatal("seeded catalog product has no SKUs")
	}
	return fixture
}

func newIntegrationMutationService(t *testing.T, store ProductMutationStore, cache DetailCache, now time.Time) *CatalogMutationService {
	t.Helper()
	service, err := NewCatalogMutationService(store, cache, func() time.Time { return now }, 100*time.Millisecond, time.Second)
	if err != nil {
		t.Fatalf("NewCatalogMutationService() error = %v", err)
	}
	return service
}

func newIntegrationInvalidationWorker(t *testing.T, db *sql.DB, cache DetailCache) *CacheInvalidationWorker {
	t.Helper()
	worker, err := NewCacheInvalidationWorker(NewCacheInvalidationRepository(db), cache, CacheInvalidationConfig{
		PollInterval:   10 * time.Millisecond,
		BatchSize:      32,
		LeaseDuration:  time.Minute,
		MaxRetries:     3,
		RetryBaseDelay: 10 * time.Millisecond,
		RetryMaxDelay:  time.Second,
		CallTimeout:    time.Second,
	})
	if err != nil {
		t.Fatalf("NewCacheInvalidationWorker() error = %v", err)
	}
	return worker
}

func preloadCacheKeys(t *testing.T, cache DetailCache, keys []string) {
	t.Helper()
	ctx := testContext(t)
	for _, key := range keys {
		if err := cache.Set(ctx, key, []byte(`{"source":"integration"}`), time.Minute); err != nil {
			t.Fatalf("preload cache key %q: %v", key, err)
		}
	}
}

func maxInvalidationTaskID(t *testing.T, db *sql.DB) uint64 {
	t.Helper()
	var id sql.NullInt64
	if err := db.QueryRowContext(testContext(t), "SELECT MAX(id) FROM cache_invalidation_tasks").Scan(&id); err != nil {
		t.Fatalf("load cache invalidation task watermark: %v", err)
	}
	if !id.Valid {
		return 0
	}
	return uint64(id.Int64)
}

type persistedInvalidationTask struct {
	id       uint64
	cacheKey string
	status   CacheInvalidationStatus
}

func loadCreatedTasks(t *testing.T, db *sql.DB, afterID uint64, expectedKeys []string) []persistedInvalidationTask {
	t.Helper()
	expected := make(map[string]struct{}, len(expectedKeys))
	for _, key := range expectedKeys {
		expected[key] = struct{}{}
	}
	rows, err := db.QueryContext(testContext(t), "SELECT id, cache_key, status FROM cache_invalidation_tasks WHERE id > ? ORDER BY id", afterID)
	if err != nil {
		t.Fatalf("load created cache invalidation tasks: %v", err)
	}
	defer rows.Close()
	var tasks []persistedInvalidationTask
	for rows.Next() {
		var task persistedInvalidationTask
		if err := rows.Scan(&task.id, &task.cacheKey, &task.status); err != nil {
			t.Fatalf("scan created cache invalidation task: %v", err)
		}
		if _, ok := expected[task.cacheKey]; ok {
			tasks = append(tasks, task)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate created cache invalidation tasks: %v", err)
	}
	if len(tasks) != len(expectedKeys) {
		t.Fatalf("created task count = %d, want %d", len(tasks), len(expectedKeys))
	}
	return tasks
}

func loadTasksByID(t *testing.T, db *sql.DB, ids []uint64) []persistedInvalidationTask {
	t.Helper()
	tasks := make([]persistedInvalidationTask, 0, len(ids))
	for _, id := range ids {
		var task persistedInvalidationTask
		err := db.QueryRowContext(testContext(t), "SELECT id, cache_key, status FROM cache_invalidation_tasks WHERE id = ?", id).Scan(&task.id, &task.cacheKey, &task.status)
		if err != nil {
			t.Fatalf("load cache invalidation task %d: %v", id, err)
		}
		tasks = append(tasks, task)
	}
	return tasks
}

func taskIDs(tasks []persistedInvalidationTask) []uint64 {
	ids := make([]uint64, 0, len(tasks))
	for _, task := range tasks {
		ids = append(ids, task.id)
	}
	return ids
}

func assertTaskStatuses(t *testing.T, tasks []persistedInvalidationTask, want CacheInvalidationStatus) {
	t.Helper()
	for _, task := range tasks {
		if task.status != want {
			t.Fatalf("task %d (%s) status = %s, want %s", task.id, task.cacheKey, task.status, want)
		}
	}
}

func assertCacheKeysExist(t *testing.T, client *redis.Client, keys []string, want bool) {
	t.Helper()
	for _, key := range keys {
		assertCacheKeyExists(t, client, key, want)
	}
}

func assertCacheKeyExists(t *testing.T, client *redis.Client, key string, want bool) {
	t.Helper()
	count, err := client.Exists(testContext(t), key).Result()
	if err != nil {
		t.Fatalf("check cache key %q: %v", key, err)
	}
	if got := count == 1; got != want {
		t.Fatalf("cache key %q exists = %v, want %v", key, got, want)
	}
}

func assertSameCacheKeys(t *testing.T, got, want []string) {
	t.Helper()
	got = append([]string(nil), got...)
	want = append([]string(nil), want...)
	sort.Strings(got)
	sort.Strings(want)
	if len(got) != len(want) {
		t.Fatalf("cache key count = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("cache keys = %v, want %v", got, want)
		}
	}
}

func testContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), integrationTimeout)
	t.Cleanup(cancel)
	return ctx
}

type failFirstDeleteCache struct {
	DetailCache
	mu       sync.Mutex
	failed   bool
	failedOn string
}

func (c *failFirstDeleteCache) Delete(ctx context.Context, key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.failed {
		c.failed = true
		c.failedOn = key
		return errors.New("injected cache delete failure")
	}
	return c.DetailCache.Delete(ctx, key)
}

func (c *failFirstDeleteCache) failedKey() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.failedOn
}
