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
