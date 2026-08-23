package outbox

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/segmentio/kafka-go"
)

type Status string

const (
	Pending    Status = "PENDING"
	Processing Status = "PROCESSING"
	Published  Status = "PUBLISHED"
	Dead       Status = "DEAD"
)

type Event struct {
	ID         uint64
	EventID    string
	Topic      string
	EventKey   string
	Payload    []byte
	Status     Status
	Attempts   int
	ClaimToken string
	LeaseUntil time.Time
}

type Repository interface {
	LeasePending(context.Context, int, time.Time, time.Duration) ([]Event, error)
	MarkPublished(context.Context, uint64, string) error
	Retry(context.Context, uint64, string, time.Time, string) error
	MarkDead(context.Context, uint64, string, string) error
}

type Message struct {
	Topic string
	Key   string
	Value []byte
}

type Publisher interface {
	Publish(context.Context, Message) error
}

type Config struct {
	BatchSize     int
	LeaseDuration time.Duration
	CallTimeout   time.Duration
	MaxAttempts   int
}

func (c Config) Validate() error {
	if c.BatchSize <= 0 {
		return errors.New("knowledge outbox batch size must be positive")
	}
	if c.LeaseDuration <= 0 {
		return errors.New("knowledge outbox lease duration must be positive")
	}
	if c.CallTimeout <= 0 {
		return errors.New("knowledge outbox call timeout must be positive")
	}
	if c.MaxAttempts <= 0 {
		return errors.New("knowledge outbox max attempts must be positive")
	}
	callCount := time.Duration(1 + 2*c.BatchSize)
	if c.CallTimeout > c.LeaseDuration/callCount {
		return errors.New("knowledge outbox call timeout exceeds lease batch budget")
	}
	return nil
}

type Worker struct {
	repository Repository
	publisher  Publisher
	config     Config
}

func NewWorker(repository Repository, publisher Publisher, config Config) *Worker {
	return &Worker{repository: repository, publisher: publisher, config: config}
}

func (w *Worker) RunOnce(ctx context.Context) error {
	if w == nil || w.repository == nil || w.publisher == nil {
		return errors.New("knowledge outbox worker is unavailable")
	}
	if err := w.config.Validate(); err != nil {
		return err
	}
	leaseCtx, cancelLease := w.callContext(ctx)
	events, err := w.repository.LeasePending(leaseCtx, w.config.BatchSize, time.Now(), w.config.LeaseDuration)
	cancelLease()
	if err != nil {
		return errors.New("lease knowledge outbox events failed")
	}
	for _, event := range events {
		publishCtx, cancelPublish := w.callContext(ctx)
		err = w.publisher.Publish(publishCtx, Message{Topic: event.Topic, Key: event.EventKey, Value: append([]byte(nil), event.Payload...)})
		cancelPublish()
		if err != nil {
			if event.Attempts >= w.config.MaxAttempts {
				deadCtx, cancelDead := w.callContext(ctx)
				deadErr := w.repository.MarkDead(deadCtx, event.ID, event.ClaimToken, classifyError(err))
				cancelDead()
				if deadErr != nil {
					return errors.New("persist knowledge outbox dead state failed")
				}
				return errors.New("publish knowledge outbox event failed")
			}
			retryCtx, cancelRetry := w.callContext(ctx)
			retryErr := w.repository.Retry(retryCtx, event.ID, event.ClaimToken, time.Now().Add(retryDelay(event.Attempts)), classifyError(err))
			cancelRetry()
			if retryErr != nil {
				return errors.New("persist knowledge outbox retry failed")
			}
			return errors.New("publish knowledge outbox event failed")
		}
		markCtx, cancelMark := w.callContext(ctx)
		err = w.repository.MarkPublished(markCtx, event.ID, event.ClaimToken)
		cancelMark()
		if err != nil {
			return errors.New("persist knowledge outbox publication failed")
		}
	}
	return nil
}

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

func (w *Worker) callContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, w.config.CallTimeout)
}

type MySQLRepository struct {
	db *sql.DB
}

func NewMySQLRepository(db *sql.DB) *MySQLRepository {
	return &MySQLRepository{db: db}
}

func (r *MySQLRepository) LeasePending(ctx context.Context, limit int, now time.Time, leaseDuration time.Duration) ([]Event, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, `SELECT id, event_id, topic, event_key, payload, attempts FROM outbox_events WHERE ((status = 'PENDING' AND (next_retry_at IS NULL OR next_retry_at <= ?)) OR (status = 'PROCESSING' AND lease_until <= ?)) ORDER BY id ASC LIMIT ? FOR UPDATE SKIP LOCKED`, now.UTC().Truncate(time.Millisecond), now.UTC().Truncate(time.Millisecond), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	events := make([]Event, 0, limit)
	for rows.Next() {
		var event Event
		if err := rows.Scan(&event.ID, &event.EventID, &event.Topic, &event.EventKey, &event.Payload, &event.Attempts); err != nil {
			return nil, err
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
		count, err := result.RowsAffected()
		if err != nil || count != 1 {
			return nil, errors.New("knowledge outbox event lease lost")
		}
		events[i].Status, events[i].Attempts, events[i].LeaseUntil, events[i].ClaimToken = Processing, events[i].Attempts+1, leaseUntil, claimToken
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return events, nil
}

func (r *MySQLRepository) MarkPublished(ctx context.Context, id uint64, claimToken string) error {
	result, err := r.db.ExecContext(ctx, `UPDATE outbox_events SET status = 'PUBLISHED', published_at = CURRENT_TIMESTAMP(3), next_retry_at = NULL, locked_at = NULL, lease_until = NULL, claim_token = NULL, last_error = NULL WHERE id = ? AND status = 'PROCESSING' AND claim_token = ?`, id, claimToken)
	return requireOneRow(result, err)
}

func (r *MySQLRepository) Retry(ctx context.Context, id uint64, claimToken string, nextRetryAt time.Time, lastError string) error {
	result, err := r.db.ExecContext(ctx, `UPDATE outbox_events SET status = 'PENDING', next_retry_at = ?, locked_at = NULL, lease_until = NULL, claim_token = NULL, last_error = ? WHERE id = ? AND status = 'PROCESSING' AND claim_token = ?`, nextRetryAt.UTC().Truncate(time.Millisecond), lastError, id, claimToken)
	return requireOneRow(result, err)
}

func (r *MySQLRepository) MarkDead(ctx context.Context, id uint64, claimToken string, lastError string) error {
	result, err := r.db.ExecContext(ctx, `UPDATE outbox_events SET status = 'DEAD', next_retry_at = NULL, locked_at = NULL, lease_until = NULL, claim_token = NULL, last_error = ? WHERE id = ? AND status = 'PROCESSING' AND claim_token = ?`, lastError, id, claimToken)
	return requireOneRow(result, err)
}

type KafkaPublisher struct {
	writer *kafka.Writer
}

func NewKafkaPublisher(brokers []string) *KafkaPublisher {
	return &KafkaPublisher{writer: &kafka.Writer{Addr: kafka.TCP(brokers...), RequiredAcks: kafka.RequireAll, Async: false}}
}

func (p *KafkaPublisher) Publish(ctx context.Context, message Message) error {
	if p == nil || p.writer == nil {
		return errors.New("kafka publisher is unavailable")
	}
	return p.writer.WriteMessages(ctx, kafka.Message{Topic: message.Topic, Key: []byte(message.Key), Value: message.Value})
}

func (p *KafkaPublisher) Close() error {
	if p == nil || p.writer == nil {
		return nil
	}
	return p.writer.Close()
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

func classifyError(err error) string {
	if err == nil {
		return ""
	}
	return "publish_failed"
}

func requireOneRow(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil || count != 1 {
		return errors.New("knowledge outbox event lease lost")
	}
	return nil
}
