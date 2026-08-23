package catalog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"time"
)

// SettlementState is the order-owned status for the exact reservation attempt.
type SettlementState string

const (
	SettlementPaid           SettlementState = "PAID"
	SettlementPendingPayment SettlementState = "PENDING_PAYMENT"
	SettlementClosed         SettlementState = "CLOSED"
	SettlementProcessing     SettlementState = "PAYMENT_PROCESSING"
)

// ExpiredReservation is a fenced lease of a product-owned expired reservation group.
type ExpiredReservation struct{ ReservationID, OrderNo, PaymentAttemptID, LeaseToken string }

// ExpiryStore owns catalog lease and fenced state transitions.
type ExpiryStore interface {
	LeaseExpired(context.Context, int, time.Time, time.Duration) ([]ExpiredReservation, error)
	ConfirmExpired(context.Context, ExpiredReservation, time.Time) error
	ReleaseExpired(context.Context, ExpiredReservation, time.Time) error
	RetryExpired(context.Context, ExpiredReservation, time.Time) error
}

// SettlementClient makes the authenticated, read-only order-service lookup.
type SettlementClient interface {
	GetPaymentSettlementStatus(context.Context, string, string) (SettlementState, error)
}

// ExpiryWorkerConfig bounds expiry reconciliation and retry scheduling.
type ExpiryWorkerConfig struct {
	BatchSize                              int
	LeaseDuration, CallTimeout, RetryDelay time.Duration
}

// ReservationExpiryWorker makes expiration safe without direct trade_db access.
type ReservationExpiryWorker struct {
	store       ExpiryStore
	settlements SettlementClient
	config      ExpiryWorkerConfig
}

// MySQLExpiryStore persists catalog-owned expiry leases and fenced transitions.
type MySQLExpiryStore struct{ db *sql.DB }

func NewMySQLExpiryStore(db *sql.DB) *MySQLExpiryStore { return &MySQLExpiryStore{db: db} }
func (s *MySQLExpiryStore) LeaseExpired(ctx context.Context, limit int, now time.Time, lease time.Duration) ([]ExpiredReservation, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("catalog database is required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT reservation_id, order_no, payment_attempt_id FROM inventory_reservations WHERE status='RESERVED' AND expires_at<=? AND (next_retry_at IS NULL OR next_retry_at<=?) AND (expiry_lease_until IS NULL OR expiry_lease_until<=?) GROUP BY reservation_id,order_no,payment_attempt_id ORDER BY reservation_id LIMIT ? FOR UPDATE SKIP LOCKED`, now, now, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []ExpiredReservation
	for rows.Next() {
		var v ExpiredReservation
		if err := rows.Scan(&v.ReservationID, &v.OrderNo, &v.PaymentAttemptID); err != nil {
			return nil, err
		}
		v.LeaseToken = uuid.NewString()
		items = append(items, v)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, v := range items {
		r, e := tx.ExecContext(ctx, `UPDATE inventory_reservations SET expiry_lease_token=?, expiry_lease_until=? WHERE reservation_id=? AND status='RESERVED' AND expires_at<=? AND (next_retry_at IS NULL OR next_retry_at<=?) AND (expiry_lease_until IS NULL OR expiry_lease_until<=?)`, v.LeaseToken, now.Add(lease), v.ReservationID, now, now, now)
		if e != nil {
			return nil, e
		}
		n, e := r.RowsAffected()
		if e != nil || n == 0 {
			return nil, errors.New("reservation expiry lease lost")
		}
	}
	return items, tx.Commit()
}
func (s *MySQLExpiryStore) ConfirmExpired(ctx context.Context, v ExpiredReservation, now time.Time) error {
	return s.transition(ctx, v, now, ReservationConfirmed, "")
}
func (s *MySQLExpiryStore) ReleaseExpired(ctx context.Context, v ExpiredReservation, now time.Time) error {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	rows, e := tx.QueryContext(ctx, `SELECT sku_id, quantity FROM inventory_reservations WHERE reservation_id=? AND status='RESERVED' AND expiry_lease_token=? FOR UPDATE`, v.ReservationID, v.LeaseToken)
	if e != nil {
		return e
	}
	defer rows.Close()
	type item struct {
		id uint64
		q  uint32
	}
	var items []item
	var reservationItems []ReservationItem
	for rows.Next() {
		var i item
		if e = rows.Scan(&i.id, &i.q); e != nil {
			return e
		}
		items = append(items, i)
		reservationItems = append(reservationItems, ReservationItem{SKUID: i.id, Quantity: i.q})
	}
	if e = rows.Err(); e != nil {
		return e
	}
	if len(items) == 0 {
		return errors.New("reservation expiry release lease lost")
	}
	for _, i := range items {
		if _, e = tx.ExecContext(ctx, releaseInventoryQuery, i.q, i.id); e != nil {
			return e
		}
	}
	r, e := tx.ExecContext(ctx, `UPDATE inventory_reservations SET status='RELEASED', released_at=?, expiry_lease_token=NULL, expiry_lease_until=NULL WHERE reservation_id=? AND status='RESERVED' AND expiry_lease_token=?`, now, v.ReservationID, v.LeaseToken)
	if e != nil {
		return e
	}
	n, e := r.RowsAffected()
	if e != nil || n != int64(len(items)) {
		return fmt.Errorf("reservation expiry release lease lost")
	}
	if _, e = createReservationInvalidationTasks(ctx, tx, reservationItems, now); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *MySQLExpiryStore) RetryExpired(ctx context.Context, v ExpiredReservation, next time.Time) error {
	r, e := s.db.ExecContext(ctx, `UPDATE inventory_reservations SET next_retry_at=?, expiry_lease_token=NULL, expiry_lease_until=NULL WHERE reservation_id=? AND status='RESERVED' AND expiry_lease_token=?`, next, v.ReservationID, v.LeaseToken)
	if e != nil {
		return e
	}
	n, e := r.RowsAffected()
	if e != nil || n == 0 {
		return errors.New("reservation expiry retry lease lost")
	}
	return nil
}
func (s *MySQLExpiryStore) transition(ctx context.Context, v ExpiredReservation, now time.Time, status ReservationStatus, _ string) error {
	r, e := s.db.ExecContext(ctx, `UPDATE inventory_reservations SET status=?, confirmed_at=?, expiry_lease_token=NULL, expiry_lease_until=NULL WHERE reservation_id=? AND status='RESERVED' AND expiry_lease_token=?`, status, now, v.ReservationID, v.LeaseToken)
	if e != nil {
		return e
	}
	n, e := r.RowsAffected()
	if e != nil || n == 0 {
		return errors.New("reservation expiry confirmation lease lost")
	}
	return nil
}

// NewReservationExpiryWorker creates the expiry reconciler.
func NewReservationExpiryWorker(store ExpiryStore, settlements SettlementClient, config ExpiryWorkerConfig) *ReservationExpiryWorker {
	return &ReservationExpiryWorker{store: store, settlements: settlements, config: config}
}

// RunOnce processes one bounded batch. Unknown/timeout states are retained, never released.
func (w *ReservationExpiryWorker) RunOnce(ctx context.Context) error {
	if w == nil || w.store == nil || w.settlements == nil || w.config.BatchSize <= 0 || w.config.LeaseDuration <= 0 || w.config.CallTimeout <= 0 || w.config.RetryDelay <= 0 {
		return errors.New("reservation expiry worker is unavailable")
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	leaseCtx, cancel := context.WithTimeout(ctx, w.config.CallTimeout)
	items, err := w.store.LeaseExpired(leaseCtx, w.config.BatchSize, now, w.config.LeaseDuration)
	cancel()
	if err != nil {
		return errors.New("lease expired reservations failed")
	}
	for _, item := range items {
		callCtx, callCancel := context.WithTimeout(ctx, w.config.CallTimeout)
		state, lookupErr := w.settlements.GetPaymentSettlementStatus(callCtx, item.OrderNo, item.PaymentAttemptID)
		callCancel()
		actionCtx, actionCancel := context.WithTimeout(ctx, w.config.CallTimeout)
		if lookupErr != nil || state == SettlementProcessing {
			err = w.store.RetryExpired(actionCtx, item, now.Add(w.config.RetryDelay))
		} else if state == SettlementPaid {
			err = w.store.ConfirmExpired(actionCtx, item, now)
		} else if state == SettlementPendingPayment || state == SettlementClosed {
			err = w.store.ReleaseExpired(actionCtx, item, now)
		} else {
			err = w.store.RetryExpired(actionCtx, item, now.Add(w.config.RetryDelay))
		}
		actionCancel()
		if err != nil {
			return errors.New("reconcile expired reservation failed")
		}
	}
	return nil
}

// Run polls expiry reconciliation until cancellation.
func (w *ReservationExpiryWorker) Run(ctx context.Context, interval time.Duration) error {
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
