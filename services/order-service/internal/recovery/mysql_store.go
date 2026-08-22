package recovery

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/yuyu945/AI-Shopping/services/order-service/internal/order"
)

// MySQLStore keeps recovery ownership in trade_db; all mutations are fenced by lease token.
type MySQLStore struct{ db *sql.DB }

func NewMySQLStore(db *sql.DB) *MySQLStore { return &MySQLStore{db: db} }
func (s *MySQLStore) LeaseStale(ctx context.Context, limit int, now time.Time, lease time.Duration) ([]Attempt, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("trade database is required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT user_id, order_no, payment_attempt_id, reservation_id FROM orders WHERE status = 'PAYMENT_PROCESSING' AND payment_started_at <= ? AND (payment_recovery_lease_until IS NULL OR payment_recovery_lease_until <= ?) ORDER BY id LIMIT ? FOR UPDATE SKIP LOCKED`, now.Add(-lease), now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []Attempt
	for rows.Next() {
		var a Attempt
		if err := rows.Scan(&a.UserID, &a.OrderNo, &a.Payment.ID, &a.Payment.ReservationID); err != nil {
			return nil, err
		}
		a.LeaseToken = uuid.NewString()
		result = append(result, a)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, a := range result {
		r, e := tx.ExecContext(ctx, `UPDATE orders SET payment_recovery_token=?, payment_recovery_lease_until=? WHERE user_id=? AND order_no=? AND status='PAYMENT_PROCESSING' AND payment_attempt_id=? AND reservation_id=? AND (payment_recovery_lease_until IS NULL OR payment_recovery_lease_until <= ?)`, a.LeaseToken, now.Add(lease), a.UserID, a.OrderNo, a.Payment.ID, a.Payment.ReservationID, now)
		if e != nil {
			return nil, e
		}
		n, e := r.RowsAffected()
		if e != nil || n != 1 {
			return nil, errors.New("payment recovery lease lost")
		}
	}
	return result, tx.Commit()
}
func (s *MySQLStore) Reset(ctx context.Context, a Attempt) (bool, error) {
	r, e := s.db.ExecContext(ctx, `UPDATE orders SET status='PENDING_PAYMENT', payment_attempt_id=NULL, reservation_id=NULL, payment_started_at=NULL, payment_recovery_token=NULL, payment_recovery_lease_until=NULL WHERE user_id=? AND order_no=? AND status='PAYMENT_PROCESSING' AND payment_attempt_id=? AND reservation_id=? AND payment_recovery_token=?`, a.UserID, a.OrderNo, a.Payment.ID, a.Payment.ReservationID, a.LeaseToken)
	if e != nil {
		return false, e
	}
	n, e := r.RowsAffected()
	return n == 1, e
}
func (s *MySQLStore) Done(ctx context.Context, a Attempt, _ time.Time) error {
	_, e := s.db.ExecContext(ctx, `UPDATE orders SET payment_recovery_token=NULL, payment_recovery_lease_until=NULL WHERE user_id=? AND order_no=? AND status='PAYMENT_PROCESSING' AND payment_attempt_id=? AND reservation_id=? AND payment_recovery_token=?`, a.UserID, a.OrderNo, a.Payment.ID, a.Payment.ReservationID, a.LeaseToken)
	return e
}

// PaymentServiceSettler adapts the order payment coordinator without exposing its repository.
type PaymentServiceSettler struct{ Service *order.PaymentService }

func (s PaymentServiceSettler) SettleRecovered(ctx context.Context, a Attempt) error {
	return s.Service.SettleRecovered(ctx, a.UserID, a.OrderNo, a.Payment)
}
