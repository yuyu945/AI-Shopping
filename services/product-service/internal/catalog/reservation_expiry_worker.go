package catalog

import (
	"context"
	"errors"
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
