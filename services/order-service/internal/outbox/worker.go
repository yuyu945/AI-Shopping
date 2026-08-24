// Package outbox publishes durable confirmation events after the payment transaction commits.
package outbox

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/segmentio/kafka-go"
)

const ConfirmationTopic = "inventory.reservation.confirm"
const ReviewEventsTopic = "review.events"

type Status string

const (
	Pending    Status = "PENDING"
	Processing Status = "PROCESSING"
	Published  Status = "PUBLISHED"
)

// Event contains only the confirmation identity needed by product-service.
type Event struct {
	ID         uint64
	EventID    string
	Topic      string
	Key        string
	Payload    []byte
	Status     Status
	Attempts   int
	LeaseUntil time.Time
	ClaimToken string
}

// Repository provides durable ownership and retry transitions for confirmation events.
type Repository interface {
	LeasePending(context.Context, int, time.Time, time.Duration) ([]Event, error)
	MarkPublished(context.Context, uint64, string) error
	Retry(context.Context, uint64, string, time.Time) error
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
		publishCtx, cancelPublish := w.callContext(ctx)
		err = w.publisher.Publish(publishCtx, Message{Topic: event.Topic, Key: event.Key, Value: append([]byte(nil), event.Payload...)})
		cancelPublish()
		if err != nil {
			retryCtx, cancelRetry := w.callContext(ctx)
			retryErr := w.repository.Retry(retryCtx, event.ID, event.ClaimToken, time.Now().Add(retryDelay(event.Attempts)))
			cancelRetry()
			if retryErr != nil {
				return errors.New("persist confirmation retry failed")
			}
			return errors.New("publish reservation confirmation failed")
		}
		markCtx, cancelMark := w.callContext(ctx)
		err = w.repository.MarkPublished(markCtx, event.ID, event.ClaimToken)
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
	topics := []string{ConfirmationTopic, ReviewEventsTopic}
	args := make([]any, 0, len(topics)+3)
	for _, topic := range topics {
		args = append(args, topic)
	}
	args = append(args, now.UTC().Truncate(time.Millisecond), now.UTC().Truncate(time.Millisecond), limit)
	rows, err := tx.QueryContext(ctx, queryLeasePendingOutboxEvents(topics), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := make([]Event, 0, limit)
	for rows.Next() {
		var event Event
		if err := rows.Scan(&event.ID, &event.EventID, &event.Topic, &event.Key, &event.Payload, &event.Attempts); err != nil {
			return nil, err
		}
		if !json.Valid(event.Payload) || event.EventID == "" || event.Topic == "" || event.Key == "" {
			return nil, fmt.Errorf("invalid outbox payload")
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range events {
		claimToken := uuid.NewString()
		leaseUntil := now.UTC().Add(leaseDuration).Truncate(time.Millisecond)
		result, err := tx.ExecContext(ctx, `UPDATE outbox_events SET status = 'PROCESSING', attempts = attempts + 1, locked_at = ?, lease_until = ?, claim_token = ? WHERE id = ? AND (status = 'PENDING' OR (status = 'PROCESSING' AND lease_until <= ?))`, now.UTC().Truncate(time.Millisecond), leaseUntil, claimToken, events[i].ID, now.UTC().Truncate(time.Millisecond))
		if err != nil {
			return nil, err
		}
		rows, err := result.RowsAffected()
		if err != nil || rows != 1 {
			return nil, errors.New("confirmation event lease lost")
		}
		events[i].Status, events[i].Attempts, events[i].LeaseUntil, events[i].ClaimToken = Processing, events[i].Attempts+1, leaseUntil, claimToken
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return events, nil
}

// MarkPublished records product acknowledgement before the event leaves the queue.
func (r *MySQLRepository) MarkPublished(ctx context.Context, id uint64, claimToken string) error {
	result, err := r.db.ExecContext(ctx, `UPDATE outbox_events SET status = 'PUBLISHED', published_at = CURRENT_TIMESTAMP(3), next_retry_at = NULL, locked_at = NULL, lease_until = NULL, claim_token = NULL WHERE id = ? AND status = 'PROCESSING' AND claim_token = ?`, id, claimToken)
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
func (r *MySQLRepository) Retry(ctx context.Context, id uint64, claimToken string, nextRetryAt time.Time) error {
	result, err := r.db.ExecContext(ctx, `UPDATE outbox_events SET status = 'PENDING', next_retry_at = ?, locked_at = NULL, lease_until = NULL, claim_token = NULL WHERE id = ? AND status = 'PROCESSING' AND claim_token = ?`, nextRetryAt.UTC().Truncate(time.Millisecond), id, claimToken)
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

func queryLeasePendingOutboxEvents(topics []string) string {
	return `SELECT id, event_id, topic, event_key, payload, attempts FROM outbox_events WHERE topic IN (` + placeholders(len(topics)) + `) AND ((status = 'PENDING' AND (next_retry_at IS NULL OR next_retry_at <= ?)) OR (status = 'PROCESSING' AND lease_until <= ?)) ORDER BY id ASC LIMIT ? FOR UPDATE SKIP LOCKED`
}

func placeholders(count int) string {
	if count <= 0 {
		return "?"
	}
	result := "?"
	for i := 1; i < count; i++ {
		result += ",?"
	}
	return result
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
