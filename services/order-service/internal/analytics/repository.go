package analytics

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

const insertReviewAnalyticsConsumption = `INSERT IGNORE INTO review_event_consumptions (event_id, consumer_group, status) VALUES (?, ?, 'PROCESSING')`
const queryReviewAnalyticsConsumption = `SELECT status FROM review_event_consumptions WHERE event_id = ? AND consumer_group = ?`
const updateReviewAnalyticsConsumptionSucceeded = `UPDATE review_event_consumptions SET status = 'SUCCEEDED', consumed_at = CURRENT_TIMESTAMP(3) WHERE event_id = ? AND consumer_group = ?`
const insertReviewEventRecord = `INSERT INTO review_event_records (event_id, review_no, order_no, user_id, product_id, sku_id, rating, content, occurred_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`
const queryProductReviewStatsForUpdate = `SELECT review_count, rating_sum FROM product_review_stats WHERE product_id = ? FOR UPDATE`
const insertProductReviewStats = `INSERT INTO product_review_stats (product_id, review_count, rating_sum, rating_avg, last_review_at) VALUES (?, ?, ?, ?, ?)`
const updateProductReviewStats = `UPDATE product_review_stats SET review_count = ?, rating_sum = ?, rating_avg = ?, last_review_at = GREATEST(COALESCE(last_review_at, ?), ?) WHERE product_id = ?`
const insertBehaviorAnalyticsConsumption = `INSERT IGNORE INTO behavior_event_consumptions (event_id, consumer_group, status) VALUES (?, ?, 'PROCESSING')`
const queryBehaviorAnalyticsConsumption = `SELECT status FROM behavior_event_consumptions WHERE event_id = ? AND consumer_group = ?`
const updateBehaviorAnalyticsConsumptionSucceeded = `UPDATE behavior_event_consumptions SET status = 'SUCCEEDED', consumed_at = CURRENT_TIMESTAMP(3) WHERE event_id = ? AND consumer_group = ?`
const insertBehaviorEventRecord = `INSERT INTO behavior_event_records (event_id, user_id, event_type, trace_id, resource_type, resource_id, payload, occurred_at) VALUES (?, ?, ?, ?, ?, ?, CAST(? AS JSON), ?)`
const insertAnalyticsDeadLetter = `INSERT INTO analytics_dead_letters (topic, event_key, reason, raw_event_base64) VALUES (?, ?, ?, ?)`
const queryRecentBehaviorEvents = `SELECT event_id, user_id, event_type, trace_id, resource_type, resource_id, occurred_at FROM behavior_event_records ORDER BY occurred_at DESC, id DESC LIMIT 20`
const queryRecentReviewEvents = `SELECT event_id, review_no, product_id, sku_id, rating, occurred_at FROM review_event_records ORDER BY occurred_at DESC, id DESC LIMIT 20`
const queryProductReviewStats = `SELECT product_id, review_count, rating_avg FROM product_review_stats ORDER BY updated_at DESC LIMIT 20`
const queryRecentDeadLetters = `SELECT topic, event_key, reason, created_at FROM analytics_dead_letters ORDER BY created_at DESC, id DESC LIMIT 20`

type MySQLRepository struct {
	db *sql.DB
}

func NewMySQLRepository(db *sql.DB) *MySQLRepository {
	return &MySQLRepository{db: db}
}

func (r *MySQLRepository) HandleReviewEvent(ctx context.Context, event ReviewEvent) (err error) {
	if r == nil || r.db == nil {
		return errors.New("review analytics database is required")
	}
	if err := event.Validate(); err != nil {
		return err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin review analytics transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	result, err := tx.ExecContext(ctx, insertReviewAnalyticsConsumption, event.EventID, ReviewConsumerGroup)
	if err != nil {
		return fmt.Errorf("insert review event consumption: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read review event consumption rows: %w", err)
	}
	if rows == 0 {
		var status string
		if err = tx.QueryRowContext(ctx, queryReviewAnalyticsConsumption, event.EventID, ReviewConsumerGroup).Scan(&status); err != nil {
			return fmt.Errorf("read review event consumption status: %w", err)
		}
		if status == "SUCCEEDED" {
			if err = tx.Commit(); err != nil {
				return fmt.Errorf("commit skipped review analytics transaction: %w", err)
			}
			return nil
		}
		return errors.New("review event consumption is already processing")
	}

	occurredAt := event.OccurredAt.UTC().Truncate(time.Millisecond)
	if _, err = tx.ExecContext(ctx, insertReviewEventRecord, event.EventID, event.ReviewNo, event.OrderNo, event.UserID, event.ProductID, event.SKUID, event.Rating, event.Content, occurredAt); err != nil {
		return fmt.Errorf("insert review event record: %w", err)
	}
	if err = r.updateProductStats(ctx, tx, event.ProductID, uint64(event.Rating), occurredAt); err != nil {
		return err
	}
	result, err = tx.ExecContext(ctx, updateReviewAnalyticsConsumptionSucceeded, event.EventID, ReviewConsumerGroup)
	if err = requireSingleRow(result, err, "mark review event consumption succeeded"); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit review analytics transaction: %w", err)
	}
	return nil
}

func (r *MySQLRepository) HandleBehaviorEvent(ctx context.Context, event BehaviorEvent) (err error) {
	if r == nil || r.db == nil {
		return errors.New("behavior analytics database is required")
	}
	if err := event.Validate(); err != nil {
		return err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin behavior analytics transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	result, err := tx.ExecContext(ctx, insertBehaviorAnalyticsConsumption, event.EventID, BehaviorConsumerGroup)
	if err != nil {
		return fmt.Errorf("insert behavior event consumption: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read behavior event consumption rows: %w", err)
	}
	if rows == 0 {
		var status string
		if err = tx.QueryRowContext(ctx, queryBehaviorAnalyticsConsumption, event.EventID, BehaviorConsumerGroup).Scan(&status); err != nil {
			return fmt.Errorf("read behavior event consumption status: %w", err)
		}
		if status == "SUCCEEDED" {
			if err = tx.Commit(); err != nil {
				return fmt.Errorf("commit skipped behavior analytics transaction: %w", err)
			}
			return nil
		}
		return errors.New("behavior event consumption is already processing")
	}

	occurredAt := event.OccurredAt.UTC().Truncate(time.Millisecond)
	if _, err = tx.ExecContext(ctx, insertBehaviorEventRecord, event.EventID, event.UserID, event.EventType, event.TraceID, event.ResourceType, event.ResourceID, string(event.Payload), occurredAt); err != nil {
		return fmt.Errorf("insert behavior event record: %w", err)
	}
	result, err = tx.ExecContext(ctx, updateBehaviorAnalyticsConsumptionSucceeded, event.EventID, BehaviorConsumerGroup)
	if err = requireSingleRow(result, err, "mark behavior event consumption succeeded"); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit behavior analytics transaction: %w", err)
	}
	return nil
}

func (r *MySQLRepository) RecordDeadLetter(ctx context.Context, record DeadLetterRecord) error {
	if r == nil || r.db == nil {
		return errors.New("analytics database is required")
	}
	result, err := r.db.ExecContext(ctx, insertAnalyticsDeadLetter, record.Topic, record.EventKey, record.Reason, record.RawEventBase64)
	return requireSingleRow(result, err, "insert analytics dead letter")
}

func (r *MySQLRepository) GetOverview(ctx context.Context, limit int) (Overview, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	behaviorEvents, err := r.listBehaviorEvents(ctx)
	if err != nil {
		return Overview{}, err
	}
	reviewEvents, err := r.listReviewEvents(ctx)
	if err != nil {
		return Overview{}, err
	}
	productStats, err := r.listProductStats(ctx)
	if err != nil {
		return Overview{}, err
	}
	deadLetters, err := r.listDeadLetters(ctx)
	if err != nil {
		return Overview{}, err
	}
	return Overview{BehaviorEvents: behaviorEvents, ReviewEvents: reviewEvents, ProductStats: productStats, DeadLetters: deadLetters}, nil
}

func (r *MySQLRepository) listBehaviorEvents(ctx context.Context) ([]BehaviorEventRecord, error) {
	rows, err := r.db.QueryContext(ctx, queryRecentBehaviorEvents)
	if err != nil {
		return nil, fmt.Errorf("read behavior event overview: %w", err)
	}
	defer rows.Close()
	var out []BehaviorEventRecord
	for rows.Next() {
		var item BehaviorEventRecord
		if err := rows.Scan(&item.EventID, &item.UserID, &item.EventType, &item.TraceID, &item.ResourceType, &item.ResourceID, &item.OccurredAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *MySQLRepository) listReviewEvents(ctx context.Context) ([]ReviewEventRecord, error) {
	rows, err := r.db.QueryContext(ctx, queryRecentReviewEvents)
	if err != nil {
		return nil, fmt.Errorf("read review event overview: %w", err)
	}
	defer rows.Close()
	var out []ReviewEventRecord
	for rows.Next() {
		var item ReviewEventRecord
		if err := rows.Scan(&item.EventID, &item.ReviewNo, &item.ProductID, &item.SKUID, &item.Rating, &item.OccurredAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *MySQLRepository) listProductStats(ctx context.Context) ([]ProductReviewStat, error) {
	rows, err := r.db.QueryContext(ctx, queryProductReviewStats)
	if err != nil {
		return nil, fmt.Errorf("read product review stats overview: %w", err)
	}
	defer rows.Close()
	var out []ProductReviewStat
	for rows.Next() {
		var item ProductReviewStat
		if err := rows.Scan(&item.ProductID, &item.ReviewCount, &item.RatingAvg); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *MySQLRepository) listDeadLetters(ctx context.Context) ([]DeadLetterOverview, error) {
	rows, err := r.db.QueryContext(ctx, queryRecentDeadLetters)
	if err != nil {
		return nil, fmt.Errorf("read dead letter overview: %w", err)
	}
	defer rows.Close()
	var out []DeadLetterOverview
	for rows.Next() {
		var item DeadLetterOverview
		if err := rows.Scan(&item.Topic, &item.EventKey, &item.Reason, &item.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *MySQLRepository) updateProductStats(ctx context.Context, tx *sql.Tx, productID, rating uint64, occurredAt time.Time) error {
	var count uint64
	var sum uint64
	err := tx.QueryRowContext(ctx, queryProductReviewStatsForUpdate, productID).Scan(&count, &sum)
	if errors.Is(err, sql.ErrNoRows) {
		result, insertErr := tx.ExecContext(ctx, insertProductReviewStats, productID, uint64(1), rating, ratingAverage(1, rating), occurredAt)
		return requireSingleRow(result, insertErr, "insert product review stats")
	}
	if err != nil {
		return fmt.Errorf("lock product review stats: %w", err)
	}
	count++
	sum += rating
	result, err := tx.ExecContext(ctx, updateProductReviewStats, count, sum, ratingAverage(count, sum), occurredAt, occurredAt, productID)
	return requireSingleRow(result, err, "update product review stats")
}

func ratingAverage(count, sum uint64) string {
	if count == 0 {
		return "0.00"
	}
	return fmt.Sprintf("%.2f", float64(sum)/float64(count))
}

func requireSingleRow(result sql.Result, err error, operation string) error {
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
