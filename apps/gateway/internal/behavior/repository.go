package behavior

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
)

const insertBehaviorOutbox = `INSERT INTO behavior_outbox_events (event_id, user_id, event_type, topic, event_key, payload) VALUES (?, ?, ?, ?, ?, CAST(? AS JSON))`
const claimBehaviorOutbox = `SELECT id, event_id, user_id, event_type, topic, event_key, payload FROM behavior_outbox_events WHERE status = 'PENDING' AND topic = 'behavior.events' AND ? IS NOT NULL AND ? IS NULL AND (next_retry_at IS NULL OR next_retry_at <= CURRENT_TIMESTAMP(3)) ORDER BY id ASC LIMIT ? FOR UPDATE`
const markBehaviorOutboxPublished = `UPDATE behavior_outbox_events SET status = 'PUBLISHED', updated_at = CURRENT_TIMESTAMP(3) WHERE claim_token = ? AND id = ?`
const markBehaviorOutboxRetry = `UPDATE behavior_outbox_events SET status = 'PENDING', attempts = attempts + 1, last_error = ?, next_retry_at = DATE_ADD(CURRENT_TIMESTAMP(3), INTERVAL 1 SECOND), updated_at = CURRENT_TIMESTAMP(3) WHERE id = ?`

type MySQLRepository struct {
	db *sql.DB
}

func NewMySQLRepository(db *sql.DB) *MySQLRepository {
	return &MySQLRepository{db: db}
}

func (r *MySQLRepository) Record(ctx context.Context, event Event) error {
	if r == nil || r.db == nil {
		return fmt.Errorf("behavior repository unavailable")
	}
	if err := event.Validate(); err != nil {
		return err
	}
	_, err := r.db.ExecContext(ctx, insertBehaviorOutbox, event.EventID, event.UserID, event.EventType, BehaviorEventsTopic, strconv.FormatUint(event.UserID, 10), string(event.Payload))
	if err != nil {
		return fmt.Errorf("insert behavior outbox event: %w", err)
	}
	return nil
}

func (r *MySQLRepository) Claim(ctx context.Context, batchSize int, claimToken string) (events []LeasedEvent, err error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("behavior repository unavailable")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin behavior claim transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	rows, err := tx.QueryContext(ctx, claimBehaviorOutbox, claimToken, nil, batchSize)
	if err != nil {
		return nil, fmt.Errorf("claim behavior outbox rows: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var event LeasedEvent
		if err := rows.Scan(&event.ID, &event.EventID, &event.UserID, &event.Type, &event.Topic, &event.Key, &event.Payload); err != nil {
			return nil, fmt.Errorf("scan behavior outbox row: %w", err)
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read behavior outbox rows: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit behavior claim transaction: %w", err)
	}
	return events, nil
}

func (r *MySQLRepository) MarkPublished(ctx context.Context, id uint64, claimToken string) error {
	result, err := r.db.ExecContext(ctx, markBehaviorOutboxPublished, claimToken, id)
	return requireOneRow(result, err, "mark behavior outbox published")
}

func (r *MySQLRepository) MarkRetry(ctx context.Context, id uint64, errText string) error {
	result, err := r.db.ExecContext(ctx, markBehaviorOutboxRetry, errText, id)
	return requireOneRow(result, err, "mark behavior outbox retry")
}

func leasedColumns() []string {
	return []string{"id", "event_id", "user_id", "event_type", "topic", "event_key", "payload"}
}

func requireOneRow(result sql.Result, err error, operation string) error {
	if err != nil {
		return fmt.Errorf("%s: %w", operation, err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("%s rows affected: %w", operation, err)
	}
	if rows != 1 {
		return fmt.Errorf("%s: row not found", operation)
	}
	return nil
}
