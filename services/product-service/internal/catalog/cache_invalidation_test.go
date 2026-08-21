package catalog

import (
	"context"
	"database/sql/driver"
	"errors"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestCacheInvalidationWorkerClaimsOnlyDueOrStaleTasks(t *testing.T) {
	now := time.Date(2026, time.August, 21, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name       string
		task       CacheInvalidationTask
		wantDelete int
	}{
		{
			name: "future pending is not claimed",
			task: CacheInvalidationTask{ID: 1, CacheKey: "future", ExecuteAt: now.Add(time.Second), Status: CacheInvalidationPending},
		},
		{
			name:       "due pending is claimed",
			task:       CacheInvalidationTask{ID: 2, CacheKey: "due", ExecuteAt: now, Status: CacheInvalidationPending},
			wantDelete: 1,
		},
		{
			name:       "stale running is reclaimed",
			task:       CacheInvalidationTask{ID: 3, CacheKey: "stale", ExecuteAt: now.Add(-time.Minute), Status: CacheInvalidationRunning, LockedAt: now.Add(-2 * time.Minute)},
			wantDelete: 1,
		},
		{
			name: "live running is not reclaimed",
			task: CacheInvalidationTask{ID: 4, CacheKey: "live", ExecuteAt: now.Add(-time.Minute), Status: CacheInvalidationRunning, LockedAt: now.Add(-30 * time.Second)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &fakeInvalidationStore{tasks: []CacheInvalidationTask{tt.task}}
			cache := &fakeInvalidationCache{}
			worker := newTestInvalidationWorker(t, store, cache, defaultInvalidationConfig())
			worker.now = func() time.Time { return now }

			if err := worker.RunOnce(context.Background(), now); err != nil {
				t.Fatalf("RunOnce() error = %v", err)
			}
			if len(cache.deleted) != tt.wantDelete {
				t.Fatalf("Delete calls = %d, want %d", len(cache.deleted), tt.wantDelete)
			}
			if len(store.done) != tt.wantDelete {
				t.Fatalf("MarkDone calls = %d, want %d", len(store.done), tt.wantDelete)
			}
			if tt.wantDelete == 1 && cache.deleted[0] != tt.task.CacheKey {
				t.Fatalf("Delete key = %q, want %q", cache.deleted[0], tt.task.CacheKey)
			}
		})
	}
}

func TestCacheInvalidationWorkerRetriesWithBoundedBackoffAndDeadLetter(t *testing.T) {
	now := time.Date(2026, time.August, 21, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name        string
		retryCount  int
		maxRetries  int
		baseDelay   time.Duration
		maxDelay    time.Duration
		wantRetry   int
		wantStatus  CacheInvalidationStatus
		wantExecute time.Time
	}{
		{
			name: "first failure uses base delay", retryCount: 0, maxRetries: 4,
			baseDelay: 10 * time.Second, maxDelay: time.Minute,
			wantRetry: 1, wantStatus: CacheInvalidationPending, wantExecute: now.Add(10 * time.Second),
		},
		{
			name: "second failure doubles and caps delay", retryCount: 1, maxRetries: 4,
			baseDelay: 10 * time.Second, maxDelay: 15 * time.Second,
			wantRetry: 2, wantStatus: CacheInvalidationPending, wantExecute: now.Add(15 * time.Second),
		},
		{
			name: "retry limit becomes dead", retryCount: 2, maxRetries: 3,
			baseDelay: 10 * time.Second, maxDelay: time.Minute,
			wantRetry: 3, wantStatus: CacheInvalidationDead, wantExecute: now,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &fakeInvalidationStore{tasks: []CacheInvalidationTask{{
				ID: 1, CacheKey: "product:v1:detail:1:sku:all", ExecuteAt: now,
				RetryCount: tt.retryCount, Status: CacheInvalidationPending,
			}}}
			cache := &fakeInvalidationCache{deleteErr: errors.New("redis password=secret unavailable")}
			config := defaultInvalidationConfig()
			config.MaxRetries = tt.maxRetries
			config.RetryBaseDelay = tt.baseDelay
			config.RetryMaxDelay = tt.maxDelay
			worker := newTestInvalidationWorker(t, store, cache, config)
			worker.now = func() time.Time { return now }

			if err := worker.RunOnce(context.Background(), now); err != nil {
				t.Fatalf("RunOnce() error = %v", err)
			}
			if len(store.failures) != 1 {
				t.Fatalf("MarkFailure calls = %d, want 1", len(store.failures))
			}
			failure := store.failures[0]
			if failure.retryCount != tt.wantRetry || failure.status != tt.wantStatus || !failure.executeAt.Equal(tt.wantExecute) {
				t.Fatalf("failure = %#v, want retry=%d status=%s execute_at=%s", failure, tt.wantRetry, tt.wantStatus, tt.wantExecute)
			}
		})
	}
}

func TestCacheInvalidationWorkerContinuesAfterDeleteFailure(t *testing.T) {
	now := time.Date(2026, time.August, 21, 12, 0, 0, 0, time.UTC)
	store := &fakeInvalidationStore{tasks: []CacheInvalidationTask{
		{ID: 1, CacheKey: "bad", ExecuteAt: now, Status: CacheInvalidationPending},
		{ID: 2, CacheKey: "good", ExecuteAt: now, Status: CacheInvalidationPending},
	}}
	cache := &fakeInvalidationCache{deleteErrors: map[string]error{"bad": errors.New("unavailable")}}
	worker := newTestInvalidationWorker(t, store, cache, defaultInvalidationConfig())
	worker.now = func() time.Time { return now }

	if err := worker.RunOnce(context.Background(), now); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if len(cache.deleted) != 2 || len(store.failures) != 1 || len(store.done) != 1 || store.done[0].id != 2 {
		t.Fatalf("unexpected processing: deleted=%v failures=%#v done=%#v", cache.deleted, store.failures, store.done)
	}
}

func TestCacheInvalidationWorkerReturnsSafeStatePersistenceError(t *testing.T) {
	now := time.Date(2026, time.August, 21, 12, 0, 0, 0, time.UTC)
	store := &fakeInvalidationStore{
		tasks:   []CacheInvalidationTask{{ID: 1, CacheKey: "bad", ExecuteAt: now, Status: CacheInvalidationPending}},
		markErr: errors.New("mysql password=secret unavailable"),
	}
	cache := &fakeInvalidationCache{deleteErr: errors.New("redis secret")}
	worker := newTestInvalidationWorker(t, store, cache, defaultInvalidationConfig())
	worker.now = func() time.Time { return now }

	err := worker.RunOnce(context.Background(), now)
	if err == nil || !strings.Contains(err.Error(), "persist cache invalidation failure") {
		t.Fatalf("RunOnce() error = %v, want safe persistence error", err)
	}
	if strings.Contains(err.Error(), "secret") {
		t.Fatalf("RunOnce() leaked dependency details: %v", err)
	}
}

func TestCacheInvalidationWorkerBoundsExternalCalls(t *testing.T) {
	now := time.Date(2026, time.August, 21, 12, 0, 0, 0, time.UTC)
	store := &fakeInvalidationStore{tasks: []CacheInvalidationTask{{ID: 1, CacheKey: "slow", ExecuteAt: now, Status: CacheInvalidationPending}}}
	cache := &fakeInvalidationCache{waitForContextDone: true}
	config := defaultInvalidationConfig()
	config.CallTimeout = 10 * time.Millisecond
	worker := newTestInvalidationWorker(t, store, cache, config)
	worker.now = func() time.Time { return now }

	if err := worker.RunOnce(context.Background(), now); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if !store.claimHadDeadline || !store.markHadDeadline {
		t.Fatalf("store deadlines: claim=%v mark=%v", store.claimHadDeadline, store.markHadDeadline)
	}
	if len(store.failures) != 1 {
		t.Fatalf("MarkFailure calls = %d, want 1", len(store.failures))
	}
}

func TestCacheInvalidationWorkerRunStopsOnCancellation(t *testing.T) {
	worker := newTestInvalidationWorker(t, &fakeInvalidationStore{}, &fakeInvalidationCache{}, defaultInvalidationConfig())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run() error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run() did not stop after cancellation")
	}
}

func TestCacheInvalidationWorkerRunRecoversAfterTransientCycleError(t *testing.T) {
	now := time.Date(2026, time.August, 21, 12, 0, 0, 0, time.UTC)
	store := &fakeInvalidationStore{
		tasks:       []CacheInvalidationTask{{ID: 1, CacheKey: "eventual", ExecuteAt: now, Status: CacheInvalidationPending}},
		claimErrors: []error{errors.New("mysql password=secret temporarily unavailable")},
		doneSignal:  make(chan struct{}),
	}
	config := defaultInvalidationConfig()
	config.PollInterval = 5 * time.Millisecond
	config.BatchSize = 1
	config.CallTimeout = 20 * time.Millisecond
	config.LeaseDuration = 20 * time.Millisecond
	worker := newTestInvalidationWorker(t, store, &fakeInvalidationCache{}, config)
	worker.now = func() time.Time { return now }

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()
	select {
	case <-store.doneSignal:
		cancel()
	case <-time.After(time.Second):
		cancel()
		t.Fatal("Run() did not recover after transient claim failure")
	}
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context.Canceled", err)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.claimCalls < 2 {
		t.Fatalf("ClaimDue calls = %d, want at least 2", store.claimCalls)
	}
	for _, claimNow := range store.claimNows {
		if !claimNow.Equal(now) {
			t.Fatalf("ClaimDue now = %s, want injected current time %s", claimNow, now)
		}
	}
}

func TestCacheInvalidationWorkerUsesFreshPostDeleteTime(t *testing.T) {
	claimNow := time.Date(2026, time.August, 21, 12, 0, 0, 0, time.UTC)
	outcomeNow := claimNow.Add(7 * time.Second)
	tests := []struct {
		name        string
		deleteErr   error
		wantDone    bool
		wantRetryAt time.Time
	}{
		{name: "successful delete marks actual completion time", wantDone: true},
		{name: "failed delete schedules from actual failure time", deleteErr: errors.New("unavailable"), wantRetryAt: outcomeNow.Add(time.Second)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &fakeInvalidationStore{tasks: []CacheInvalidationTask{{ID: 1, CacheKey: "key", ExecuteAt: claimNow, Status: CacheInvalidationPending}}}
			worker := newTestInvalidationWorker(t, store, &fakeInvalidationCache{deleteErr: tt.deleteErr}, defaultInvalidationConfig())
			worker.now = func() time.Time { return outcomeNow }

			if err := worker.RunOnce(context.Background(), claimNow); err != nil {
				t.Fatalf("RunOnce() error = %v", err)
			}
			if tt.wantDone {
				if len(store.done) != 1 || !store.done[0].doneAt.Equal(outcomeNow) {
					t.Fatalf("MarkDone calls = %#v, want done_at %s", store.done, outcomeNow)
				}
				return
			}
			if len(store.failures) != 1 || !store.failures[0].executeAt.Equal(tt.wantRetryAt) {
				t.Fatalf("MarkFailure calls = %#v, want execute_at %s", store.failures, tt.wantRetryAt)
			}
		})
	}
}

func TestNewCacheInvalidationWorkerValidatesConfig(t *testing.T) {
	valid := defaultInvalidationConfig()
	tests := []struct {
		name   string
		store  CacheInvalidationTaskStore
		cache  DetailCache
		mutate func(*CacheInvalidationConfig)
	}{
		{name: "nil store", cache: &fakeInvalidationCache{}},
		{name: "nil cache", store: &fakeInvalidationStore{}},
		{name: "poll interval", store: &fakeInvalidationStore{}, cache: &fakeInvalidationCache{}, mutate: func(c *CacheInvalidationConfig) { c.PollInterval = 0 }},
		{name: "negative poll interval", store: &fakeInvalidationStore{}, cache: &fakeInvalidationCache{}, mutate: func(c *CacheInvalidationConfig) { c.PollInterval = -time.Second }},
		{name: "batch size", store: &fakeInvalidationStore{}, cache: &fakeInvalidationCache{}, mutate: func(c *CacheInvalidationConfig) { c.BatchSize = 0 }},
		{name: "lease duration", store: &fakeInvalidationStore{}, cache: &fakeInvalidationCache{}, mutate: func(c *CacheInvalidationConfig) { c.LeaseDuration = 0 }},
		{name: "negative lease duration", store: &fakeInvalidationStore{}, cache: &fakeInvalidationCache{}, mutate: func(c *CacheInvalidationConfig) { c.LeaseDuration = -time.Second }},
		{name: "max retries", store: &fakeInvalidationStore{}, cache: &fakeInvalidationCache{}, mutate: func(c *CacheInvalidationConfig) { c.MaxRetries = 0 }},
		{name: "retry base delay", store: &fakeInvalidationStore{}, cache: &fakeInvalidationCache{}, mutate: func(c *CacheInvalidationConfig) { c.RetryBaseDelay = 0 }},
		{name: "negative retry base delay", store: &fakeInvalidationStore{}, cache: &fakeInvalidationCache{}, mutate: func(c *CacheInvalidationConfig) { c.RetryBaseDelay = -time.Second }},
		{name: "retry max delay", store: &fakeInvalidationStore{}, cache: &fakeInvalidationCache{}, mutate: func(c *CacheInvalidationConfig) { c.RetryMaxDelay = 0 }},
		{name: "negative retry max delay", store: &fakeInvalidationStore{}, cache: &fakeInvalidationCache{}, mutate: func(c *CacheInvalidationConfig) { c.RetryMaxDelay = -time.Second }},
		{name: "base exceeds max", store: &fakeInvalidationStore{}, cache: &fakeInvalidationCache{}, mutate: func(c *CacheInvalidationConfig) { c.RetryBaseDelay = c.RetryMaxDelay + time.Second }},
		{name: "call timeout", store: &fakeInvalidationStore{}, cache: &fakeInvalidationCache{}, mutate: func(c *CacheInvalidationConfig) { c.CallTimeout = 0 }},
		{name: "negative call timeout", store: &fakeInvalidationStore{}, cache: &fakeInvalidationCache{}, mutate: func(c *CacheInvalidationConfig) { c.CallTimeout = -time.Second }},
		{name: "lease shorter than batch budget", store: &fakeInvalidationStore{}, cache: &fakeInvalidationCache{}, mutate: func(c *CacheInvalidationConfig) {
			c.LeaseDuration = c.CallTimeout*time.Duration(c.BatchSize) - time.Nanosecond
		}},
		{name: "batch budget overflows duration", store: &fakeInvalidationStore{}, cache: &fakeInvalidationCache{}, mutate: func(c *CacheInvalidationConfig) {
			c.BatchSize = 2
			c.CallTimeout = time.Duration(1<<63 - 1)
			c.LeaseDuration = time.Duration(1<<63 - 1)
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := valid
			if tt.mutate != nil {
				tt.mutate(&config)
			}
			worker, err := NewCacheInvalidationWorker(tt.store, tt.cache, config)
			if err == nil || worker != nil {
				t.Fatalf("NewCacheInvalidationWorker() = %#v, %v, want nil worker and error", worker, err)
			}
		})
	}
}

func TestCacheInvalidationRepositoryClaimDueUsesLeaseAndSkipLocked(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Date(2026, time.August, 21, 12, 0, 0, 123456789, time.UTC)
	leaseNow := time.Date(2026, time.August, 21, 12, 0, 0, 123000000, time.UTC)
	staleBefore := leaseNow.Add(-time.Minute)

	mock.ExpectBegin()
	claimSQL := regexp.QuoteMeta("SELECT id, cache_key, execute_at, retry_count, status, locked_at FROM cache_invalidation_tasks WHERE (status = ? AND execute_at <= ?) OR (status = ? AND locked_at <= ?) ORDER BY execute_at ASC, id ASC LIMIT ? FOR UPDATE SKIP LOCKED")
	mock.ExpectQuery(claimSQL).
		WithArgs(CacheInvalidationPending, leaseNow, CacheInvalidationRunning, staleBefore, 2).
		WillReturnRows(sqlmock.NewRows([]string{"id", "cache_key", "execute_at", "retry_count", "status", "locked_at"}).
			AddRow(uint64(4), "key-4", now.Add(-2*time.Minute), 0, CacheInvalidationPending, nil).
			AddRow(uint64(9), "key-9", now.Add(-time.Minute), 1, CacheInvalidationRunning, staleBefore.Add(-time.Second)))
	claimUpdate := regexp.QuoteMeta("UPDATE cache_invalidation_tasks SET status = ?, locked_at = ? WHERE id = ?")
	mock.ExpectExec(claimUpdate).WithArgs(CacheInvalidationRunning, leaseNow, uint64(4)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(claimUpdate).WithArgs(CacheInvalidationRunning, leaseNow, uint64(9)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	tasks, err := NewCacheInvalidationRepository(db).ClaimDue(context.Background(), now, time.Minute, 2)
	if err != nil {
		t.Fatalf("ClaimDue() error = %v", err)
	}
	if len(tasks) != 2 || tasks[0].ID != 4 || tasks[1].ID != 9 {
		t.Fatalf("ClaimDue() = %#v", tasks)
	}
	for _, task := range tasks {
		if task.Status != CacheInvalidationRunning || !task.LockedAt.Equal(leaseNow) {
			t.Fatalf("claimed task lease = %#v, want RUNNING at %s", task, leaseNow)
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCacheInvalidationRepositoryMarkDoneRequiresCurrentLease(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	lease := time.Date(2026, time.August, 21, 12, 0, 0, 123456789, time.UTC)
	normalizedLease := lease.Truncate(time.Millisecond)
	doneAt := lease.Add(time.Second)
	normalizedDoneAt := doneAt.Truncate(time.Millisecond)

	query := regexp.QuoteMeta("UPDATE cache_invalidation_tasks SET status = ?, locked_at = NULL, last_error = NULL, executed_at = ? WHERE id = ? AND status = ? AND locked_at = ?")
	mock.ExpectExec(query).
		WithArgs(CacheInvalidationDone, normalizedDoneAt, uint64(7), CacheInvalidationRunning, normalizedLease).
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := NewCacheInvalidationRepository(db).MarkDone(context.Background(), 7, lease, doneAt); err != nil {
		t.Fatalf("MarkDone() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCacheInvalidationRepositoryMarkFailureUsesSafeErrorAndCurrentLease(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	lease := time.Date(2026, time.August, 21, 12, 0, 0, 123456789, time.UTC)
	normalizedLease := lease.Truncate(time.Millisecond)
	retryAt := lease.Add(time.Minute)
	normalizedRetryAt := retryAt.Truncate(time.Millisecond)

	tests := []struct {
		name   string
		dead   bool
		status CacheInvalidationStatus
		query  string
		args   []driver.Value
	}{
		{
			name: "pending retry", status: CacheInvalidationPending,
			query: "UPDATE cache_invalidation_tasks SET retry_count = ?, status = ?, execute_at = ?, last_error = ?, locked_at = NULL WHERE id = ? AND status = ? AND locked_at = ?",
			args:  []driver.Value{2, CacheInvalidationPending, normalizedRetryAt, "cache delete failed", uint64(8), CacheInvalidationRunning, normalizedLease},
		},
		{
			name: "dead", dead: true, status: CacheInvalidationDead,
			query: "UPDATE cache_invalidation_tasks SET retry_count = ?, status = ?, last_error = ?, locked_at = NULL WHERE id = ? AND status = ? AND locked_at = ?",
			args:  []driver.Value{2, CacheInvalidationDead, "cache delete failed", uint64(8), CacheInvalidationRunning, normalizedLease},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock.ExpectExec(regexp.QuoteMeta(tt.query)).WithArgs(tt.args...).WillReturnResult(sqlmock.NewResult(0, 1))
			err := NewCacheInvalidationRepository(db).MarkFailure(context.Background(), 8, lease, 2, retryAt, tt.dead)
			if err != nil {
				t.Fatalf("MarkFailure() error = %v", err)
			}
		})
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

type fakeInvalidationStore struct {
	mu               sync.Mutex
	tasks            []CacheInvalidationTask
	done             []doneCall
	failures         []failureCall
	claimErr         error
	claimErrors      []error
	claimCalls       int
	claimNows        []time.Time
	markErr          error
	claimHadDeadline bool
	markHadDeadline  bool
	doneSignal       chan struct{}
	doneOnce         sync.Once
}

type doneCall struct {
	id      uint64
	leaseAt time.Time
	doneAt  time.Time
}

type failureCall struct {
	id         uint64
	leaseAt    time.Time
	retryCount int
	executeAt  time.Time
	dead       bool
	status     CacheInvalidationStatus
}

func (s *fakeInvalidationStore) ClaimDue(ctx context.Context, now time.Time, lease time.Duration, limit int) ([]CacheInvalidationTask, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, s.claimHadDeadline = ctx.Deadline()
	s.claimNows = append(s.claimNows, now)
	claimCall := s.claimCalls
	s.claimCalls++
	if claimCall < len(s.claimErrors) && s.claimErrors[claimCall] != nil {
		return nil, s.claimErrors[claimCall]
	}
	if s.claimErr != nil {
		return nil, s.claimErr
	}
	claimed := make([]CacheInvalidationTask, 0, limit)
	for i := range s.tasks {
		task := &s.tasks[i]
		due := task.Status == CacheInvalidationPending && !task.ExecuteAt.After(now)
		stale := task.Status == CacheInvalidationRunning && !task.LockedAt.After(now.Add(-lease))
		if !due && !stale {
			continue
		}
		task.Status = CacheInvalidationRunning
		task.LockedAt = now
		claimed = append(claimed, *task)
		if len(claimed) == limit {
			break
		}
	}
	return claimed, nil
}

func (s *fakeInvalidationStore) MarkDone(ctx context.Context, id uint64, leaseAt, doneAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, s.markHadDeadline = ctx.Deadline()
	if s.markErr != nil {
		return s.markErr
	}
	s.done = append(s.done, doneCall{id: id, leaseAt: leaseAt, doneAt: doneAt})
	if s.doneSignal != nil {
		s.doneOnce.Do(func() { close(s.doneSignal) })
	}
	return nil
}

func (s *fakeInvalidationStore) MarkFailure(ctx context.Context, id uint64, leaseAt time.Time, retryCount int, executeAt time.Time, dead bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, s.markHadDeadline = ctx.Deadline()
	if s.markErr != nil {
		return s.markErr
	}
	status := CacheInvalidationPending
	if dead {
		status = CacheInvalidationDead
	}
	s.failures = append(s.failures, failureCall{id: id, leaseAt: leaseAt, retryCount: retryCount, executeAt: executeAt, dead: dead, status: status})
	return nil
}

type fakeInvalidationCache struct {
	mu                 sync.Mutex
	deleted            []string
	deleteErr          error
	deleteErrors       map[string]error
	waitForContextDone bool
}

func (c *fakeInvalidationCache) Get(context.Context, string) ([]byte, error) {
	return nil, ErrCacheMiss
}
func (c *fakeInvalidationCache) Set(context.Context, string, []byte, time.Duration) error { return nil }

func (c *fakeInvalidationCache) Delete(ctx context.Context, key string) error {
	if c.waitForContextDone {
		<-ctx.Done()
		return ctx.Err()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.deleted = append(c.deleted, key)
	if err := c.deleteErrors[key]; err != nil {
		return err
	}
	return c.deleteErr
}

func defaultInvalidationConfig() CacheInvalidationConfig {
	return CacheInvalidationConfig{
		PollInterval:   time.Second,
		BatchSize:      10,
		LeaseDuration:  time.Minute,
		MaxRetries:     3,
		RetryBaseDelay: time.Second,
		RetryMaxDelay:  time.Minute,
		CallTimeout:    time.Second,
	}
}

func newTestInvalidationWorker(t *testing.T, store CacheInvalidationTaskStore, cache DetailCache, config CacheInvalidationConfig) *CacheInvalidationWorker {
	t.Helper()
	worker, err := NewCacheInvalidationWorker(store, cache, config)
	if err != nil {
		t.Fatalf("NewCacheInvalidationWorker() error = %v", err)
	}
	return worker
}
