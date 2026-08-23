package catalog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"time"
)

const (
	reservationRowsQuery              = "SELECT reservation_id, order_no, payment_attempt_id, sku_id, quantity, status, expires_at, confirmed_at, released_at FROM inventory_reservations WHERE reservation_id = ? ORDER BY sku_id ASC"
	reservationRowsForUpdateQuery     = reservationRowsQuery + " FOR UPDATE"
	reserveInventoryQuery             = "UPDATE inventory SET available_qty = available_qty - ?, version = version + 1 WHERE sku_id = ? AND available_qty >= ?"
	releaseInventoryQuery             = "UPDATE inventory SET available_qty = available_qty + ?, version = version + 1 WHERE sku_id = ?"
	insertReservationQuery            = "INSERT INTO inventory_reservations (reservation_id, order_no, payment_attempt_id, sku_id, quantity, status, expires_at) VALUES (?, ?, ?, ?, ?, ?, ?)"
	confirmReservationRowsQuery       = "UPDATE inventory_reservations SET status = ?, confirmed_at = ? WHERE reservation_id = ? AND status = ?"
	insertReservationConsumptionQuery = "INSERT IGNORE INTO event_consumptions (event_id, consumer_group) VALUES (?, ?)"
	releaseReservationRowsQuery       = "UPDATE inventory_reservations SET status = ?, released_at = ? WHERE reservation_id = ? AND status = ?"
	reservationProductIDQuery         = "SELECT product_id FROM product_skus WHERE id = ?"
)

// ReservationRepository is the MySQL persistence implementation for catalog-owned reservations.
type ReservationRepository struct{ db *sql.DB }

// NewReservationRepository constructs a MySQL-backed reservation store.
func NewReservationRepository(db *sql.DB) *ReservationRepository {
	return &ReservationRepository{db: db}
}

// ReserveStock creates the whole reservation group atomically or rolls all inventory changes back.
func (r *ReservationRepository) ReserveStock(ctx context.Context, input ReserveStockInput, now, executeAt time.Time) (Reservation, ReservationMutation, error) {
	input.Items = append([]ReservationItem(nil), input.Items...)
	sort.Slice(input.Items, func(i, j int) bool { return input.Items[i].SKUID < input.Items[j].SKUID })
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Reservation{}, ReservationMutation{}, errors.New("begin inventory reservation failed")
	}
	defer func() { _ = tx.Rollback() }()
	existing, err := loadReservation(ctx, tx, reservationRowsForUpdateQuery, input.ReservationID)
	if err != nil && !errors.Is(err, ErrReservationNotFound) {
		return Reservation{}, ReservationMutation{}, err
	}
	if len(existing.Items) > 0 {
		if !sameReservationPayload(existing, input) {
			return Reservation{}, ReservationMutation{}, ErrReservationConflict
		}
		if err := tx.Commit(); err != nil {
			return Reservation{}, ReservationMutation{}, errors.New("commit reservation replay failed")
		}
		return existing, ReservationMutation{}, nil
	}
	for _, item := range input.Items {
		result, err := tx.ExecContext(ctx, reserveInventoryQuery, item.Quantity, item.SKUID, item.Quantity)
		if err != nil {
			return Reservation{}, ReservationMutation{}, errors.New("reserve inventory failed")
		}
		affected, err := result.RowsAffected()
		if err != nil || affected != 1 {
			return Reservation{}, ReservationMutation{}, ErrReservationOutOfStock
		}
	}
	for _, item := range input.Items {
		if _, err := tx.ExecContext(ctx, insertReservationQuery, input.ReservationID, input.OrderNo, input.PaymentAttemptID, item.SKUID, item.Quantity, ReservationReserved, input.ExpiresAt); err != nil {
			return Reservation{}, ReservationMutation{}, errors.New("insert inventory reservation failed")
		}
	}
	reservation := Reservation{ReservationID: input.ReservationID, OrderNo: input.OrderNo, PaymentAttemptID: input.PaymentAttemptID, Status: ReservationReserved, ExpiresAt: input.ExpiresAt, Items: append([]ReservationItem(nil), input.Items...)}
	mutation, err := createReservationInvalidationTasks(ctx, tx, input.Items, executeAt)
	if err != nil {
		return Reservation{}, ReservationMutation{}, err
	}
	if err := tx.Commit(); err != nil {
		return Reservation{}, ReservationMutation{}, errors.New("commit inventory reservation failed")
	}
	return reservation, mutation, nil
}

// ConfirmReservation changes an entire reserved group to confirmed once.
func (r *ReservationRepository) ConfirmReservation(ctx context.Context, reservationID string, now time.Time) (Reservation, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Reservation{}, errors.New("begin reservation confirmation failed")
	}
	defer func() { _ = tx.Rollback() }()
	reservation, err := confirmReservationTx(ctx, tx, reservationID, now)
	if err != nil {
		return Reservation{}, err
	}
	if err := tx.Commit(); err != nil {
		return Reservation{}, errors.New("commit reservation confirmation failed")
	}
	return reservation, nil
}

// ConfirmConsumed confirms a reservation and records its Kafka event in the same catalog transaction.
func (r *ReservationRepository) ConfirmConsumed(ctx context.Context, eventID, group, reservationID, orderNo, paymentAttemptID string, now time.Time) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return errors.New("begin consumed reservation confirmation failed")
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, insertReservationConsumptionQuery, eventID, group)
	if err != nil {
		return errors.New("record inventory confirmation event failed")
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return errors.New("read inventory confirmation event result failed")
	}
	if rows == 0 {
		if err := tx.Commit(); err != nil {
			return errors.New("commit inventory confirmation replay failed")
		}
		return nil
	}
	reservation, err := loadReservation(ctx, tx, reservationRowsForUpdateQuery, reservationID)
	if err != nil {
		return err
	}
	if reservation.ReservationID != reservationID || reservation.OrderNo != orderNo || reservation.PaymentAttemptID != paymentAttemptID {
		return ErrReservationConflict
	}
	if _, err := confirmLoadedReservationTx(ctx, tx, reservation, now); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return errors.New("commit consumed reservation confirmation failed")
	}
	return nil
}

func confirmReservationTx(ctx context.Context, tx *sql.Tx, reservationID string, now time.Time) (Reservation, error) {
	reservation, err := loadReservation(ctx, tx, reservationRowsForUpdateQuery, reservationID)
	if err != nil {
		return Reservation{}, err
	}
	return confirmLoadedReservationTx(ctx, tx, reservation, now)
}

func confirmLoadedReservationTx(ctx context.Context, tx *sql.Tx, reservation Reservation, now time.Time) (Reservation, error) {
	switch reservation.Status {
	case ReservationConfirmed:
		return reservation, nil
	case ReservationReserved:
		result, err := tx.ExecContext(ctx, confirmReservationRowsQuery, ReservationConfirmed, now, reservation.ReservationID, ReservationReserved)
		if err != nil {
			return Reservation{}, errors.New("confirm inventory reservation failed")
		}
		if err := exactlyRows(result, len(reservation.Items)); err != nil {
			return Reservation{}, err
		}
		reservation.Status, reservation.ConfirmedAt = ReservationConfirmed, timePointer(now)
		return reservation, nil
	default:
		return Reservation{}, ErrReservationState
	}
}

// ReleaseReservation restores a still-reserved group in one transaction and never restores a released replay.
func (r *ReservationRepository) ReleaseReservation(ctx context.Context, reservationID string, now, executeAt time.Time) (Reservation, ReservationMutation, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Reservation{}, ReservationMutation{}, errors.New("begin reservation release failed")
	}
	defer func() { _ = tx.Rollback() }()
	reservation, err := loadReservation(ctx, tx, reservationRowsForUpdateQuery, reservationID)
	if err != nil {
		return Reservation{}, ReservationMutation{}, err
	}
	switch reservation.Status {
	case ReservationReleased:
		if err := tx.Commit(); err != nil {
			return Reservation{}, ReservationMutation{}, errors.New("commit reservation release replay failed")
		}
		return reservation, ReservationMutation{}, nil
	case ReservationReserved:
		for _, item := range reservation.Items {
			result, err := tx.ExecContext(ctx, releaseInventoryQuery, item.Quantity, item.SKUID)
			if err != nil {
				return Reservation{}, ReservationMutation{}, errors.New("restore inventory failed")
			}
			if err := exactlyRows(result, 1); err != nil {
				return Reservation{}, ReservationMutation{}, err
			}
		}
		result, err := tx.ExecContext(ctx, releaseReservationRowsQuery, ReservationReleased, now, reservationID, ReservationReserved)
		if err != nil {
			return Reservation{}, ReservationMutation{}, errors.New("release inventory reservation failed")
		}
		if err := exactlyRows(result, len(reservation.Items)); err != nil {
			return Reservation{}, ReservationMutation{}, err
		}
		mutation, err := createReservationInvalidationTasks(ctx, tx, reservation.Items, executeAt)
		if err != nil {
			return Reservation{}, ReservationMutation{}, err
		}
		reservation.Status, reservation.ReleasedAt = ReservationReleased, timePointer(now)
		if err := tx.Commit(); err != nil {
			return Reservation{}, ReservationMutation{}, errors.New("commit reservation release failed")
		}
		return reservation, mutation, nil
	default:
		return Reservation{}, ReservationMutation{}, ErrReservationState
	}
}

// GetReservation reads a full reservation group with stable SKU ordering.
func (r *ReservationRepository) GetReservation(ctx context.Context, reservationID string) (Reservation, error) {
	return loadReservation(ctx, r.db, reservationRowsQuery, reservationID)
}

type reservationQuerier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func loadReservation(ctx context.Context, q reservationQuerier, query, reservationID string) (Reservation, error) {
	rows, err := q.QueryContext(ctx, query, reservationID)
	if err != nil {
		return Reservation{}, errors.New("load inventory reservation failed")
	}
	defer rows.Close()
	var reservation Reservation
	for rows.Next() {
		var status string
		var confirmedAt, releasedAt sql.NullTime
		var item ReservationItem
		var rowExpires time.Time
		if err := rows.Scan(&reservation.ReservationID, &reservation.OrderNo, &reservation.PaymentAttemptID, &item.SKUID, &item.Quantity, &status, &rowExpires, &confirmedAt, &releasedAt); err != nil {
			return Reservation{}, errors.New("scan inventory reservation failed")
		}
		if reservation.Status != "" && reservation.Status != ReservationStatus(status) {
			return Reservation{}, ErrReservationState
		}
		reservation.Status, reservation.ExpiresAt = ReservationStatus(status), normalizeMySQLTimestamp(rowExpires)
		if confirmedAt.Valid {
			at := normalizeMySQLTimestamp(confirmedAt.Time)
			reservation.ConfirmedAt = &at
		}
		if releasedAt.Valid {
			at := normalizeMySQLTimestamp(releasedAt.Time)
			reservation.ReleasedAt = &at
		}
		reservation.Items = append(reservation.Items, item)
	}
	if err := rows.Err(); err != nil {
		return Reservation{}, errors.New("iterate inventory reservation failed")
	}
	if len(reservation.Items) == 0 {
		return Reservation{}, ErrReservationNotFound
	}
	if reservation.Status != ReservationReserved && reservation.Status != ReservationConfirmed && reservation.Status != ReservationReleased {
		return Reservation{}, ErrReservationState
	}
	return reservation, nil
}

func sameReservationPayload(existing Reservation, input ReserveStockInput) bool {
	if existing.ReservationID != input.ReservationID || existing.OrderNo != input.OrderNo || existing.PaymentAttemptID != input.PaymentAttemptID || !existing.ExpiresAt.Equal(input.ExpiresAt) || len(existing.Items) != len(input.Items) {
		return false
	}
	for i := range existing.Items {
		if existing.Items[i] != input.Items[i] {
			return false
		}
	}
	return true
}

func createReservationInvalidationTasks(ctx context.Context, tx *sql.Tx, items []ReservationItem, executeAt time.Time) (ReservationMutation, error) {
	productBySKU := make(map[uint64]uint64, len(items))
	productIDs := make([]uint64, 0, len(items))
	seenProduct := make(map[uint64]struct{}, len(items))
	for _, item := range items {
		var productID uint64
		if err := tx.QueryRowContext(ctx, reservationProductIDQuery, item.SKUID).Scan(&productID); err != nil {
			return ReservationMutation{}, errors.New("load reservation product failed")
		}
		productBySKU[item.SKUID] = productID
		if _, ok := seenProduct[productID]; !ok {
			seenProduct[productID] = struct{}{}
			productIDs = append(productIDs, productID)
		}
	}
	sort.Slice(productIDs, func(i, j int) bool { return productIDs[i] < productIDs[j] })
	keys := make([]string, 0, len(productIDs)+len(items))
	for _, productID := range productIDs {
		keys = append(keys, ProductCacheKey(productID, nil))
	}
	for _, item := range items {
		skuID := item.SKUID
		keys = append(keys, ProductCacheKey(productBySKU[item.SKUID], &skuID))
	}
	mutation := ReservationMutation{CacheKeys: keys, TaskIDs: make([]uint64, 0, len(keys))}
	for _, key := range keys {
		result, err := tx.ExecContext(ctx, insertInvalidationTaskQuery, key, executeAt)
		if err != nil {
			return ReservationMutation{}, errors.New("create inventory cache invalidation task failed")
		}
		id, err := result.LastInsertId()
		if err != nil || id <= 0 {
			return ReservationMutation{}, errors.New("read inventory cache invalidation task ID failed")
		}
		mutation.TaskIDs = append(mutation.TaskIDs, uint64(id))
	}
	return mutation, nil
}

func exactlyRows(result sql.Result, want int) error {
	affected, err := result.RowsAffected()
	if err != nil || affected != int64(want) {
		return fmt.Errorf("reservation transition affected unexpected rows")
	}
	return nil
}

func timePointer(value time.Time) *time.Time { return &value }
