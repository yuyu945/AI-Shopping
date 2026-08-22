// Package outbox publishes durable confirmation events after the payment transaction commits.
package outbox

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

const confirmationTopic = "inventory.reservation.confirm"

type Status string

const (
	Pending    Status = "PENDING"
	Processing Status = "PROCESSING"
	Published  Status = "PUBLISHED"
)

// Event contains only the confirmation identity needed by product-service.
type Event struct {
	ID            uint64
	EventID       string
	ReservationID string
	Status        Status
	Attempts      int
}

// Repository provides durable ownership and retry transitions for confirmation events.
type Repository interface {
	LeasePending(context.Context, int) ([]Event, error)
	MarkPublished(context.Context, uint64) error
	Retry(context.Context, uint64, time.Time) error
}

// Publisher confirms a product-owned reservation. It intentionally has no release operation.
type Publisher interface {
	ConfirmReservation(context.Context, string) error
}

// Config limits a single polling pass.
type Config struct{ BatchSize int }

// Worker confirms paid reservations from committed outbox rows.
type Worker struct {
	repository Repository
	publisher  Publisher
	config     Config
}

// NewWorker constructs the minimal confirmation publisher.
func NewWorker(repository Repository, publisher Publisher, config Config) *Worker {
	if config.BatchSize <= 0 {
		config.BatchSize = 20
	}
	return &Worker{repository: repository, publisher: publisher, config: config}
}

// RunOnce processes one claimed batch. Failures are persisted for a later retry and returned to the caller.
func (w *Worker) RunOnce(ctx context.Context) error {
	if w == nil || w.repository == nil || w.publisher == nil {
		return errors.New("confirmation outbox worker is unavailable")
	}
	events, err := w.repository.LeasePending(ctx, w.config.BatchSize)
	if err != nil {
		return errors.New("lease confirmation outbox events failed")
	}
	for _, event := range events {
		if err := w.publisher.ConfirmReservation(ctx, event.ReservationID); err != nil {
			if retryErr := w.repository.Retry(ctx, event.ID, time.Now().Add(retryDelay(event.Attempts))); retryErr != nil {
				return errors.New("persist confirmation retry failed")
			}
			return errors.New("publish reservation confirmation failed")
		}
		if err := w.repository.MarkPublished(ctx, event.ID); err != nil {
			return errors.New("persist confirmation publication failed")
		}
	}
	return nil
}

// MySQLRepository persists confirmation events in order-service's trade_db outbox.
type MySQLRepository struct{ db *sql.DB }

// NewMySQLRepository creates a confirmation Outbox repository.
func NewMySQLRepository(db *sql.DB) *MySQLRepository { return &MySQLRepository{db: db} }

// LeasePending claims pending confirmation rows. Product confirmation is idempotent, so a duplicate after a process crash is safe.
func (r *MySQLRepository) LeasePending(ctx context.Context, limit int) ([]Event, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("outbox database is required")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT id, event_id, event_key, attempts FROM outbox_events WHERE topic = ? AND status = 'PENDING' AND (next_retry_at IS NULL OR next_retry_at <= CURRENT_TIMESTAMP(3)) ORDER BY id ASC LIMIT ? FOR UPDATE SKIP LOCKED`, confirmationTopic, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := make([]Event, 0, limit)
	for rows.Next() {
		var event Event
		if err := rows.Scan(&event.ID, &event.EventID, &event.ReservationID, &event.Attempts); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range events {
		if _, err := tx.ExecContext(ctx, `UPDATE outbox_events SET status = 'PROCESSING', attempts = attempts + 1 WHERE id = ? AND status = 'PENDING'`, events[i].ID); err != nil {
			return nil, err
		}
		events[i].Status, events[i].Attempts = Processing, events[i].Attempts+1
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return events, nil
}

// MarkPublished records product acknowledgement before the event leaves the queue.
func (r *MySQLRepository) MarkPublished(ctx context.Context, id uint64) error {
	result, err := r.db.ExecContext(ctx, `UPDATE outbox_events SET status = 'PUBLISHED', published_at = CURRENT_TIMESTAMP(3), next_retry_at = NULL WHERE id = ? AND status = 'PROCESSING'`, id)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil || rows != 1 {
		return errors.New("confirmation event lease lost")
	}
	return nil
}

// Retry makes an unacknowledged confirmation eligible again with a small bounded delay.
func (r *MySQLRepository) Retry(ctx context.Context, id uint64, nextRetryAt time.Time) error {
	result, err := r.db.ExecContext(ctx, `UPDATE outbox_events SET status = 'PENDING', next_retry_at = ? WHERE id = ? AND status = 'PROCESSING'`, nextRetryAt.UTC().Truncate(time.Millisecond), id)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil || rows != 1 {
		return errors.New("confirmation event lease lost")
	}
	return nil
}

func retryDelay(attempts int) time.Duration {
	delay := time.Second
	for attempts > 1 && delay < 30*time.Second {
		delay *= 2
		attempts--
	}
	if delay > 30*time.Second {
		return 30 * time.Second
	}
	return delay
}

// Run polls confirmation events until canceled.
func (w *Worker) Run(ctx context.Context, interval time.Duration) error {
	if interval <= 0 {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := w.RunOnce(ctx); err != nil && ctx.Err() != nil {
			return ctx.Err()
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
