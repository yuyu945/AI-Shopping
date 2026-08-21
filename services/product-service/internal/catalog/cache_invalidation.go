package catalog

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

const (
	claimCacheInvalidationTasksQuery = "SELECT id, cache_key, execute_at, retry_count, status, locked_at FROM cache_invalidation_tasks WHERE (status = ? AND execute_at <= ?) OR (status = ? AND locked_at <= ?) ORDER BY execute_at ASC, id ASC LIMIT ? FOR UPDATE SKIP LOCKED"
	leaseCacheInvalidationTaskQuery  = "UPDATE cache_invalidation_tasks SET status = ?, locked_at = ? WHERE id = ?"
	markCacheInvalidationDoneQuery   = "UPDATE cache_invalidation_tasks SET status = ?, locked_at = NULL, last_error = NULL, executed_at = ? WHERE id = ? AND status = ? AND locked_at = ?"
	markCacheInvalidationRetryQuery  = "UPDATE cache_invalidation_tasks SET retry_count = ?, status = ?, execute_at = ?, last_error = ?, locked_at = NULL WHERE id = ? AND status = ? AND locked_at = ?"
	markCacheInvalidationDeadQuery   = "UPDATE cache_invalidation_tasks SET retry_count = ?, status = ?, last_error = ?, locked_at = NULL WHERE id = ? AND status = ? AND locked_at = ?"
	cacheDeleteFailureMessage        = "cache delete failed"
)

// CacheInvalidationStatus is the persisted lifecycle state of an invalidation task.
type CacheInvalidationStatus string

const (
	// CacheInvalidationPending identifies a task waiting for its execute time.
	CacheInvalidationPending CacheInvalidationStatus = "PENDING"
	// CacheInvalidationRunning identifies a task held by a worker lease.
	CacheInvalidationRunning CacheInvalidationStatus = "RUNNING"
	// CacheInvalidationDone identifies a successfully executed task.
	CacheInvalidationDone CacheInvalidationStatus = "DONE"
	// CacheInvalidationDead identifies a task that exhausted its retry budget.
	CacheInvalidationDead CacheInvalidationStatus = "DEAD"
)

// CacheInvalidationTask describes one durable cache deletion attempt.
type CacheInvalidationTask struct {
	ID         uint64
	CacheKey   string
	ExecuteAt  time.Time
	RetryCount int
	Status     CacheInvalidationStatus
	LockedAt   time.Time
}

// CacheInvalidationConfig controls polling, leases, retries, and dependency timeouts.
type CacheInvalidationConfig struct {
	PollInterval   time.Duration
	BatchSize      int
	LeaseDuration  time.Duration
	MaxRetries     int
	RetryBaseDelay time.Duration
	RetryMaxDelay  time.Duration
	CallTimeout    time.Duration
}

// CacheInvalidationTaskStore leases tasks and persists lease-guarded outcomes.
type CacheInvalidationTaskStore interface {
	ClaimDue(context.Context, time.Time, time.Duration, int) ([]CacheInvalidationTask, error)
	MarkDone(context.Context, uint64, time.Time, time.Time) error
	MarkFailure(context.Context, uint64, time.Time, int, time.Time, bool) error
}

// CacheInvalidationRepository persists invalidation worker state in MySQL.
type CacheInvalidationRepository struct {
	db *sql.DB
}

// NewCacheInvalidationRepository constructs a MySQL-backed invalidation task store.
func NewCacheInvalidationRepository(db *sql.DB) *CacheInvalidationRepository {
	return &CacheInvalidationRepository{db: db}
}

// ClaimDue leases due pending tasks and running tasks whose previous lease expired.
func (r *CacheInvalidationRepository) ClaimDue(ctx context.Context, now time.Time, leaseDuration time.Duration, limit int) ([]CacheInvalidationTask, error) {
	leaseNow := normalizeMySQLTimestamp(now)
	staleBefore := normalizeMySQLTimestamp(leaseNow.Add(-leaseDuration))
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, errors.New("begin cache invalidation claim failed")
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.QueryContext(
		ctx,
		claimCacheInvalidationTasksQuery,
		CacheInvalidationPending,
		leaseNow,
		CacheInvalidationRunning,
		staleBefore,
		limit,
	)
	if err != nil {
		return nil, errors.New("select cache invalidation tasks failed")
	}

	tasks := make([]CacheInvalidationTask, 0, limit)
	for rows.Next() {
		var task CacheInvalidationTask
		var status string
		var previousLease sql.NullTime
		if err := rows.Scan(&task.ID, &task.CacheKey, &task.ExecuteAt, &task.RetryCount, &status, &previousLease); err != nil {
			_ = rows.Close()
			return nil, errors.New("scan cache invalidation task failed")
		}
		task.Status = CacheInvalidationStatus(status)
		tasks = append(tasks, task)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, errors.New("iterate cache invalidation tasks failed")
	}
	if err := rows.Close(); err != nil {
		return nil, errors.New("close cache invalidation task rows failed")
	}

	for i := range tasks {
		result, err := tx.ExecContext(ctx, leaseCacheInvalidationTaskQuery, CacheInvalidationRunning, leaseNow, tasks[i].ID)
		if err != nil {
			return nil, errors.New("lease cache invalidation task failed")
		}
		if err := requireOneLeaseRow(result); err != nil {
			return nil, err
		}
		tasks[i].Status = CacheInvalidationRunning
		tasks[i].LockedAt = leaseNow
	}
	if err := tx.Commit(); err != nil {
		return nil, errors.New("commit cache invalidation claim failed")
	}
	return tasks, nil
}

// MarkDone completes a task only while the caller still owns its lease.
func (r *CacheInvalidationRepository) MarkDone(ctx context.Context, id uint64, leaseAt, doneAt time.Time) error {
	leaseAt = normalizeMySQLTimestamp(leaseAt)
	doneAt = normalizeMySQLTimestamp(doneAt)
	result, err := r.db.ExecContext(
		ctx,
		markCacheInvalidationDoneQuery,
		CacheInvalidationDone,
		doneAt,
		id,
		CacheInvalidationRunning,
		leaseAt,
	)
	if err != nil {
		return errors.New("mark cache invalidation done failed")
	}
	return requireOneLeaseRow(result)
}

// MarkFailure schedules a retry or marks a task dead while the caller owns its lease.
func (r *CacheInvalidationRepository) MarkFailure(ctx context.Context, id uint64, leaseAt time.Time, retryCount int, executeAt time.Time, dead bool) error {
	leaseAt = normalizeMySQLTimestamp(leaseAt)
	executeAt = normalizeMySQLTimestamp(executeAt)
	var (
		result sql.Result
		err    error
	)
	if dead {
		result, err = r.db.ExecContext(
			ctx,
			markCacheInvalidationDeadQuery,
			retryCount,
			CacheInvalidationDead,
			cacheDeleteFailureMessage,
			id,
			CacheInvalidationRunning,
			leaseAt,
		)
	} else {
		result, err = r.db.ExecContext(
			ctx,
			markCacheInvalidationRetryQuery,
			retryCount,
			CacheInvalidationPending,
			executeAt,
			cacheDeleteFailureMessage,
			id,
			CacheInvalidationRunning,
			leaseAt,
		)
	}
	if err != nil {
		return errors.New("mark cache invalidation failure failed")
	}
	return requireOneLeaseRow(result)
}

func requireOneLeaseRow(result sql.Result) error {
	affected, err := result.RowsAffected()
	if err != nil || affected != 1 {
		return errors.New("cache invalidation lease no longer owned")
	}
	return nil
}

func normalizeMySQLTimestamp(value time.Time) time.Time {
	return value.UTC().Truncate(time.Millisecond)
}

// CacheInvalidationWorker polls durable tasks and deletes cache keys under bounded calls.
type CacheInvalidationWorker struct {
	store  CacheInvalidationTaskStore
	cache  DetailCache
	config CacheInvalidationConfig
	now    func() time.Time
}

// NewCacheInvalidationWorker validates dependencies and constructs an invalidation worker.
func NewCacheInvalidationWorker(store CacheInvalidationTaskStore, cache DetailCache, config CacheInvalidationConfig) (*CacheInvalidationWorker, error) {
	if store == nil {
		return nil, errors.New("cache invalidation store is required")
	}
	if cache == nil {
		return nil, errors.New("cache is required")
	}
	if config.PollInterval <= 0 {
		return nil, errors.New("poll interval must be positive")
	}
	if config.BatchSize <= 0 {
		return nil, errors.New("batch size must be positive")
	}
	if config.LeaseDuration <= 0 {
		return nil, errors.New("lease duration must be positive")
	}
	if config.MaxRetries <= 0 {
		return nil, errors.New("max retries must be positive")
	}
	if config.RetryBaseDelay <= 0 {
		return nil, errors.New("retry base delay must be positive")
	}
	if config.RetryMaxDelay <= 0 {
		return nil, errors.New("retry max delay must be positive")
	}
	if config.RetryBaseDelay > config.RetryMaxDelay {
		return nil, errors.New("retry base delay must not exceed retry max delay")
	}
	if config.CallTimeout <= 0 {
		return nil, errors.New("call timeout must be positive")
	}
	if config.CallTimeout > time.Duration((1<<63-1)/int64(config.BatchSize)) {
		return nil, errors.New("batch call timeout budget exceeds duration limit")
	}
	if config.LeaseDuration < config.CallTimeout*time.Duration(config.BatchSize) {
		return nil, errors.New("lease duration must cover the batch call timeout budget")
	}
	return &CacheInvalidationWorker{store: store, cache: cache, config: config, now: time.Now}, nil
}

// Run polls until the context is canceled, retrying transient cycle failures on later ticks.
func (w *CacheInvalidationWorker) Run(ctx context.Context) error {
	ticker := time.NewTicker(w.config.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := w.RunOnce(ctx, w.now()); err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				continue
			}
		}
	}
}

// RunOnce leases one batch and processes every task in lease order.
func (w *CacheInvalidationWorker) RunOnce(ctx context.Context, now time.Time) error {
	claimCtx, cancelClaim := context.WithTimeout(ctx, w.config.CallTimeout)
	tasks, err := w.store.ClaimDue(claimCtx, now, w.config.LeaseDuration, w.config.BatchSize)
	cancelClaim()
	if err != nil {
		return errors.New("claim cache invalidation tasks failed")
	}

	for _, task := range tasks {
		deleteCtx, cancelDelete := context.WithTimeout(ctx, w.config.CallTimeout)
		deleteErr := w.cache.Delete(deleteCtx, task.CacheKey)
		cancelDelete()
		outcomeAt := w.now()

		if deleteErr == nil {
			markCtx, cancelMark := context.WithTimeout(ctx, w.config.CallTimeout)
			err = w.store.MarkDone(markCtx, task.ID, task.LockedAt, outcomeAt)
			cancelMark()
			if err != nil {
				return errors.New("persist cache invalidation completion failed")
			}
			continue
		}

		newRetryCount := task.RetryCount + 1
		dead := newRetryCount >= w.config.MaxRetries
		executeAt := outcomeAt
		if !dead {
			executeAt = outcomeAt.Add(cacheInvalidationRetryDelay(newRetryCount, w.config.RetryBaseDelay, w.config.RetryMaxDelay))
		}
		markCtx, cancelMark := context.WithTimeout(ctx, w.config.CallTimeout)
		err = w.store.MarkFailure(markCtx, task.ID, task.LockedAt, newRetryCount, executeAt, dead)
		cancelMark()
		if err != nil {
			return errors.New("persist cache invalidation failure failed")
		}
	}
	return nil
}

func cacheInvalidationRetryDelay(retryCount int, baseDelay, maxDelay time.Duration) time.Duration {
	delay := baseDelay
	for retry := 1; retry < retryCount; retry++ {
		if delay >= maxDelay || delay > maxDelay-delay {
			return maxDelay
		}
		delay *= 2
	}
	if delay > maxDelay {
		return maxDelay
	}
	return delay
}
