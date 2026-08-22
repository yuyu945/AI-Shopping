// Package outbox publishes durable confirmation events after the payment transaction commits.
package outbox

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/segmentio/kafka-go"
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
	ID               uint64
	EventID          string
	ReservationID    string
	Status           Status
	Attempts         int
	OrderNo          string
	PaymentAttemptID string
	Version          int
	LeaseUntil       time.Time
}

// Repository provides durable ownership and retry transitions for confirmation events.
type Repository interface {
	LeasePending(context.Context, int, time.Time, time.Duration) ([]Event, error)
	MarkPublished(context.Context, uint64) error
	Retry(context.Context, uint64, time.Time) error
}

// Message is the stable confirmation event sent to Kafka.
type Message struct {
	Topic string
	Key   string
	Value []byte
}

// Publisher publishes a Kafka message and returns only after the producer has acknowledged it.
type Publisher interface {
	Publish(context.Context, Message) error
}

// Config limits a single polling pass.
type Config struct {
	BatchSize     int
	LeaseDuration time.Duration
	CallTimeout   time.Duration
}

// Validate ensures every serial call in a claimed batch finishes before its lease can expire.
func (c Config) Validate() error {
	if c.BatchSize <= 0 {
		return errors.New("confirmation outbox batch size must be positive")
	}
	if c.LeaseDuration <= 0 {
		return errors.New("confirmation outbox lease duration must be positive")
	}
	if c.CallTimeout <= 0 {
		return errors.New("confirmation outbox call timeout must be positive")
	}
	// One lease, then one publish and one durable state transition per event.
	callCount := time.Duration(1 + 2*c.BatchSize)
	if c.CallTimeout > c.LeaseDuration/callCount {
		return errors.New("confirmation outbox call timeout exceeds lease batch budget")
	}
	return nil
}

// Worker confirms paid reservations from committed outbox rows.
type Worker struct {
	repository Repository
	publisher  Publisher
	config     Config
}

// NewWorker constructs the minimal confirmation publisher.
func NewWorker(repository Repository, publisher Publisher, config Config) *Worker {
	return &Worker{repository: repository, publisher: publisher, config: config}
}

// RunOnce processes one claimed batch. Failures are persisted for a later retry and returned to the caller.
func (w *Worker) RunOnce(ctx context.Context) error {
	if w == nil || w.repository == nil || w.publisher == nil {
		return errors.New("confirmation outbox worker is unavailable")
	}
	if err := w.config.Validate(); err != nil {
		return err
	}
	claimCtx, cancelClaim := w.callContext(ctx)
	events, err := w.repository.LeasePending(claimCtx, w.config.BatchSize, time.Now(), w.config.LeaseDuration)
	cancelClaim()
	if err != nil {
		return errors.New("lease confirmation outbox events failed")
	}
	for _, event := range events {
		payload, err := json.Marshal(struct {
			EventID          string `json:"event_id"`
			ReservationID    string `json:"reservation_id"`
			OrderNo          string `json:"order_no"`
			PaymentAttemptID string `json:"payment_attempt_id"`
			Version          int    `json:"version"`
		}{event.EventID, event.ReservationID, event.OrderNo, event.PaymentAttemptID, event.Version})
		if err != nil {
			return errors.New("marshal reservation confirmation failed")
		}
		publishCtx, cancelPublish := w.callContext(ctx)
		err = w.publisher.Publish(publishCtx, Message{Topic: confirmationTopic, Key: event.ReservationID, Value: payload})
		cancelPublish()
		if err != nil {
			retryCtx, cancelRetry := w.callContext(ctx)
			retryErr := w.repository.Retry(retryCtx, event.ID, time.Now().Add(retryDelay(event.Attempts)))
			cancelRetry()
			if retryErr != nil {
				return errors.New("persist confirmation retry failed")
			}
			return errors.New("publish reservation confirmation failed")
		}
		markCtx, cancelMark := w.callContext(ctx)
		err = w.repository.MarkPublished(markCtx, event.ID)
		cancelMark()
		if err != nil {
			return errors.New("persist confirmation publication failed")
		}
	}
	return nil
}

func (w *Worker) callContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, w.config.CallTimeout)
}

// MySQLRepository persists confirmation events in order-service's trade_db outbox.
type MySQLRepository struct{ db *sql.DB }

// NewMySQLRepository creates a confirmation Outbox repository.
func NewMySQLRepository(db *sql.DB) *MySQLRepository { return &MySQLRepository{db: db} }

// LeasePending claims pending confirmation rows. Product confirmation is idempotent, so a duplicate after a process crash is safe.
func (r *MySQLRepository) LeasePending(ctx context.Context, limit int, now time.Time, leaseDuration time.Duration) ([]Event, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("outbox database is required")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT id, event_id, event_key, payload, attempts FROM outbox_events WHERE topic = ? AND ((status = 'PENDING' AND (next_retry_at IS NULL OR next_retry_at <= ?)) OR (status = 'PROCESSING' AND lease_until <= ?)) ORDER BY id ASC LIMIT ? FOR UPDATE SKIP LOCKED`, confirmationTopic, now.UTC().Truncate(time.Millisecond), now.UTC().Truncate(time.Millisecond), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := make([]Event, 0, limit)
	for rows.Next() {
		var event Event
		var payload struct {
			EventID          string `json:"event_id"`
			ReservationID    string `json:"reservation_id"`
			OrderNo          string `json:"order_no"`
			PaymentAttemptID string `json:"payment_attempt_id"`
			Version          int    `json:"version"`
		}
		var rawPayload []byte
		if err := rows.Scan(&event.ID, &event.EventID, &event.ReservationID, &rawPayload, &event.Attempts); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(rawPayload, &payload); err != nil {
			return nil, fmt.Errorf("decode confirmation outbox payload: %w", err)
		}
		if payload.EventID != event.EventID || payload.ReservationID != event.ReservationID || payload.OrderNo == "" || payload.PaymentAttemptID == "" || payload.Version <= 0 {
			return nil, errors.New("invalid confirmation outbox payload")
		}
		event.OrderNo, event.PaymentAttemptID, event.Version = payload.OrderNo, payload.PaymentAttemptID, payload.Version
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range events {
		leaseUntil := now.UTC().Add(leaseDuration).Truncate(time.Millisecond)
		if _, err := tx.ExecContext(ctx, `UPDATE outbox_events SET status = 'PROCESSING', attempts = attempts + 1, locked_at = ?, lease_until = ? WHERE id = ? AND (status = 'PENDING' OR (status = 'PROCESSING' AND lease_until <= ?))`, now.UTC().Truncate(time.Millisecond), leaseUntil, events[i].ID, now.UTC().Truncate(time.Millisecond)); err != nil {
			return nil, err
		}
		events[i].Status, events[i].Attempts, events[i].LeaseUntil = Processing, events[i].Attempts+1, leaseUntil
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return events, nil
}

// MarkPublished records product acknowledgement before the event leaves the queue.
func (r *MySQLRepository) MarkPublished(ctx context.Context, id uint64) error {
	result, err := r.db.ExecContext(ctx, `UPDATE outbox_events SET status = 'PUBLISHED', published_at = CURRENT_TIMESTAMP(3), next_retry_at = NULL, locked_at = NULL, lease_until = NULL WHERE id = ? AND status = 'PROCESSING'`, id)
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
	result, err := r.db.ExecContext(ctx, `UPDATE outbox_events SET status = 'PENDING', next_retry_at = ?, locked_at = NULL, lease_until = NULL WHERE id = ? AND status = 'PROCESSING'`, nextRetryAt.UTC().Truncate(time.Millisecond), id)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil || rows != 1 {
		return errors.New("confirmation event lease lost")
	}
	return nil
}

// KafkaPublisher adapts kafka-go's synchronous writer to the outbox publisher contract.
type KafkaPublisher struct{ writer *kafka.Writer }

// NewKafkaPublisher creates a producer that acknowledges delivery before returning.
func NewKafkaPublisher(brokers []string) *KafkaPublisher {
	return &KafkaPublisher{writer: &kafka.Writer{Addr: kafka.TCP(brokers...), RequiredAcks: kafka.RequireAll, Async: false}}
}

// Close releases the underlying Kafka producer resources.
func (p *KafkaPublisher) Close() error {
	if p == nil || p.writer == nil {
		return nil
	}
	return p.writer.Close()
}

// Publish waits for Kafka's producer acknowledgement.
func (p *KafkaPublisher) Publish(ctx context.Context, message Message) error {
	if p == nil || p.writer == nil {
		return errors.New("kafka publisher is unavailable")
	}
	return p.writer.WriteMessages(ctx, kafka.Message{Topic: message.Topic, Key: []byte(message.Key), Value: message.Value})
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
