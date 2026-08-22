package order

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

const queryPaymentOrderForUpdate = `SELECT id, order_no, user_id, status, total_amount, paid_amount, payment_attempt_id, reservation_id FROM orders WHERE user_id = ? AND order_no = ? FOR UPDATE`
const queryPaymentOrder = `SELECT id, order_no, user_id, status, total_amount, paid_amount, payment_attempt_id, reservation_id FROM orders WHERE user_id = ? AND order_no = ?`
const queryWalletForUpdate = `SELECT balance, version FROM wallet_accounts WHERE user_id = ? FOR UPDATE`
const claimPaymentOrder = `UPDATE orders SET status = ?, payment_attempt_id = ?, reservation_id = ?, payment_started_at = CURRENT_TIMESTAMP(3) WHERE id = ? AND user_id = ? AND status = ?`
const resetPaymentOrder = `UPDATE orders SET status = ?, payment_attempt_id = NULL, reservation_id = NULL, payment_started_at = NULL WHERE user_id = ? AND order_no = ? AND status = ? AND payment_attempt_id = ? AND reservation_id = ?`
const updateWalletBalance = `UPDATE wallet_accounts SET balance = ?, version = version + 1 WHERE user_id = ? AND version = ?`
const insertWalletLedger = `INSERT INTO wallet_ledger (user_id, biz_type, biz_id, direction, amount) VALUES (?, ?, ?, ?, ?)`
const updateOrderPaid = `UPDATE orders SET status = 'PAID', paid_amount = ?, paid_at = CURRENT_TIMESTAMP(3) WHERE id = ? AND status = 'PAYMENT_PROCESSING' AND payment_attempt_id = ? AND reservation_id = ?`
const insertReservationConfirmationOutbox = `INSERT INTO outbox_events (event_id, aggregate_type, aggregate_id, event_type, topic, event_key, payload) VALUES (?, ?, ?, ?, ?, ?, CAST(? AS JSON))`

// ClaimPayment creates the durable payment attempt in a short trade_db transaction.
func (r *MySQLRepository) ClaimPayment(ctx context.Context, userID uint64, orderNo string, attempt PaymentAttempt) (claimed Order, err error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Order{}, fmt.Errorf("begin payment claim transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	order, err := scanPaymentOrder(tx.QueryRowContext(ctx, queryPaymentOrderForUpdate, userID, orderNo))
	if errors.Is(err, sql.ErrNoRows) {
		return Order{}, ErrNotFound
	}
	if err != nil {
		return Order{}, fmt.Errorf("lock order payment: %w", err)
	}
	switch order.Status {
	case Paid:
		if err = tx.Commit(); err != nil {
			return Order{}, fmt.Errorf("commit paid payment replay: %w", err)
		}
		return order, nil
	case PaymentProcessing:
		return Order{}, ErrPaymentInProgress
	case PendingPayment:
	default:
		return Order{}, &Error{Code: IdempotencyConflict, Message: "order cannot be paid"}
	}
	result, err := tx.ExecContext(ctx, claimPaymentOrder, PaymentProcessing, attempt.ID, attempt.ReservationID, order.ID, userID, PendingPayment)
	if err != nil {
		return Order{}, fmt.Errorf("claim order payment: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return Order{}, fmt.Errorf("read claimed order rows: %w", err)
	}
	if rows != 1 {
		return Order{}, ErrPaymentInProgress
	}
	if err = tx.Commit(); err != nil {
		return Order{}, fmt.Errorf("commit payment claim: %w", err)
	}
	order.Status, order.Payment = PaymentProcessing, attempt
	order.Items, err = r.loadOrderItems(ctx, order.ID)
	if err != nil {
		return Order{}, err
	}
	return order, nil
}

// ResetPaymentClaim clears only the exact durable claim and reports whether its CAS applied.
func (r *MySQLRepository) ResetPaymentClaim(ctx context.Context, userID uint64, orderNo string, attempt PaymentAttempt) (bool, error) {
	result, err := r.db.ExecContext(ctx, resetPaymentOrder, PendingPayment, userID, orderNo, PaymentProcessing, attempt.ID, attempt.ReservationID)
	if err != nil {
		return false, fmt.Errorf("reset payment claim: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("read reset payment claim rows: %w", err)
	}
	return rows == 1, nil
}

// GetPaymentOrder reads the current durable order state after a payment claim CAS miss.
func (r *MySQLRepository) GetPaymentOrder(ctx context.Context, userID uint64, orderNo string) (Order, error) {
	order, err := scanPaymentOrder(r.db.QueryRowContext(ctx, queryPaymentOrder, userID, orderNo))
	if errors.Is(err, sql.ErrNoRows) {
		return Order{}, ErrNotFound
	}
	if err != nil {
		return Order{}, fmt.Errorf("read payment order: %w", err)
	}
	return order, nil
}

// SettleWalletPayment atomically settles a matching claim, marks PAID, and records confirmation.
func (r *MySQLRepository) SettleWalletPayment(ctx context.Context, userID uint64, orderNo string, attempt PaymentAttempt) (settled Order, err error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Order{}, fmt.Errorf("begin wallet settlement transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	order, err := scanPaymentOrder(tx.QueryRowContext(ctx, queryPaymentOrderForUpdate, userID, orderNo))
	if errors.Is(err, sql.ErrNoRows) {
		return Order{}, ErrNotFound
	}
	if err != nil {
		return Order{}, fmt.Errorf("lock order settlement: %w", err)
	}
	if order.Status == Paid {
		if err = tx.Commit(); err != nil {
			return Order{}, fmt.Errorf("commit paid settlement replay: %w", err)
		}
		return order, nil
	}
	if order.Status != PaymentProcessing || order.Payment != attempt {
		return Order{}, ErrPaymentInProgress
	}
	total, validTotal := parseMoney(order.TotalAmount)
	if !validTotal {
		return Order{}, errors.New("stored wallet or order amount is invalid")
	}
	var result sql.Result
	var rows int64
	// Fully discounted orders still settle the claim and confirm inventory without a fictitious money movement.
	if total.Sign() > 0 {
		var balanceText string
		var version uint64
		if err = tx.QueryRowContext(ctx, queryWalletForUpdate, userID).Scan(&balanceText, &version); errors.Is(err, sql.ErrNoRows) {
			return Order{}, ErrInsufficientBalance
		} else if err != nil {
			return Order{}, fmt.Errorf("lock wallet: %w", err)
		}
		balance, validBalance := parseMoney(balanceText)
		if !validBalance {
			return Order{}, errors.New("stored wallet or order amount is invalid")
		}
		if balance.Cmp(total) < 0 {
			return Order{}, ErrInsufficientBalance
		}
		remaining, moneyErr := moneyString(balance.Sub(balance, total))
		if moneyErr != nil {
			return Order{}, fmt.Errorf("calculate wallet balance: %w", moneyErr)
		}
		result, err = tx.ExecContext(ctx, updateWalletBalance, remaining, userID, version)
		if err != nil {
			return Order{}, fmt.Errorf("debit wallet: %w", err)
		}
		rows, err = result.RowsAffected()
		if err != nil {
			return Order{}, fmt.Errorf("read wallet debit rows: %w", err)
		}
		if rows != 1 {
			return Order{}, errors.New("wallet changed during settlement")
		}
		if _, err = tx.ExecContext(ctx, insertWalletLedger, userID, "ORDER_PAYMENT", orderNo, "DEBIT", order.TotalAmount); err != nil {
			return Order{}, fmt.Errorf("insert wallet ledger: %w", err)
		}
	}
	result, err = tx.ExecContext(ctx, updateOrderPaid, order.TotalAmount, order.ID, attempt.ID, attempt.ReservationID)
	if err != nil {
		return Order{}, fmt.Errorf("mark order paid: %w", err)
	}
	rows, err = result.RowsAffected()
	if err != nil {
		return Order{}, fmt.Errorf("read paid order rows: %w", err)
	}
	if rows != 1 {
		return Order{}, ErrPaymentInProgress
	}
	payload, err := json.Marshal(struct {
		ReservationID string `json:"reservation_id"`
	}{ReservationID: attempt.ReservationID})
	if err != nil {
		return Order{}, fmt.Errorf("marshal reservation confirmation: %w", err)
	}
	if _, err = tx.ExecContext(ctx, insertReservationConfirmationOutbox, attempt.ReservationID, "INVENTORY_RESERVATION", attempt.ReservationID, "CONFIRM", "inventory.reservation.confirm", attempt.ReservationID, string(payload)); err != nil {
		return Order{}, fmt.Errorf("insert reservation confirmation outbox: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return Order{}, fmt.Errorf("commit wallet settlement: %w", err)
	}
	order.Status, order.PaidAmount = Paid, order.TotalAmount
	order.Items, err = r.loadOrderItems(ctx, order.ID)
	if err != nil {
		return Order{}, err
	}
	return order, nil
}

func scanPaymentOrder(row scanner) (Order, error) {
	var order Order
	var paymentAttemptID, reservationID sql.NullString
	if err := row.Scan(&order.ID, &order.OrderNo, &order.UserID, &order.Status, &order.TotalAmount, &order.PaidAmount, &paymentAttemptID, &reservationID); err != nil {
		return Order{}, err
	}
	if paymentAttemptID.Valid != reservationID.Valid {
		return Order{}, errors.New("stored payment claim is invalid")
	}
	if paymentAttemptID.Valid {
		order.Payment = PaymentAttempt{ID: paymentAttemptID.String, ReservationID: reservationID.String}
	}
	if _, valid := parseMoney(order.TotalAmount); !valid {
		return Order{}, errors.New("stored order amount is invalid")
	}
	return order, nil
}
