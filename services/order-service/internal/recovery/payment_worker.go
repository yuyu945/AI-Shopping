// Package recovery reconciles payment claims after an order-service interruption.
package recovery

import (
	"context"
	"errors"
	"time"

	"github.com/yuyu945/AI-Shopping/services/order-service/internal/order"
)

// Attempt is a fenced lease of one durable PAYMENT_PROCESSING claim.
type Attempt struct {
	UserID     uint64
	OrderNo    string
	Payment    order.PaymentAttempt
	LeaseToken string
}

// Store owns only trade_db lease and exact-claim reset transitions.
type Store interface {
	LeaseStale(context.Context, int, time.Time, time.Duration) ([]Attempt, error)
	Reset(context.Context, Attempt) (bool, error)
	Done(context.Context, Attempt, time.Time) error
}

// Reservations reads and compensates product-owned reservations.
type Reservations interface {
	GetReservation(context.Context, string) (order.Reservation, error)
	ReleaseReservation(context.Context, string) error
}

// Settler resumes the existing, exact payment attempt without creating a new claim.
type Settler interface {
	SettleRecovered(context.Context, Attempt) error
}

// Config bounds each polling pass and every dependency call.
type Config struct {
	BatchSize                  int
	LeaseDuration, CallTimeout time.Duration
}

const worstCaseCallsPerAttempt int64 = 4

// Validate checks that one leased batch has enough time budget for serial reconciliation.
func (c Config) Validate() error {
	if c.BatchSize <= 0 {
		return errors.New("batch size must be positive")
	}
	if c.LeaseDuration <= 0 {
		return errors.New("lease duration must be positive")
	}
	if c.CallTimeout <= 0 {
		return errors.New("call timeout must be positive")
	}
	if int64(c.BatchSize) > (1<<63-1)/worstCaseCallsPerAttempt {
		return errors.New("batch call timeout budget exceeds duration limit")
	}
	calls := time.Duration(int64(c.BatchSize) * worstCaseCallsPerAttempt)
	if c.CallTimeout > time.Duration(1<<63-1)/calls {
		return errors.New("batch call timeout budget exceeds duration limit")
	}
	if c.LeaseDuration < c.CallTimeout*calls {
		return errors.New("lease duration must cover the batch call timeout budget")
	}
	return nil
}

// Worker resolves stale claims without sharing transactions with product-service.
type Worker struct {
	store        Store
	reservations Reservations
	settler      Settler
	config       Config
}

// NewWorker constructs the recovery worker.
func NewWorker(store Store, reservations Reservations, settler Settler, config Config) *Worker {
	return &Worker{store: store, reservations: reservations, settler: settler, config: config}
}

// RunOnce processes a bounded, fenced batch. A dependency failure leaves the claim recoverable.
func (w *Worker) RunOnce(ctx context.Context) error {
	if err := w.validate(); err != nil {
		return err
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	leaseCtx, cancel := context.WithTimeout(ctx, w.config.CallTimeout)
	attempts, err := w.store.LeaseStale(leaseCtx, w.config.BatchSize, now, w.config.LeaseDuration)
	cancel()
	if err != nil {
		return errors.New("lease stale payment attempts failed")
	}
	for _, attempt := range attempts {
		if err := w.reconcile(ctx, attempt); err != nil {
			return err
		}
	}
	return nil
}

// Run polls recovery until cancellation.
func (w *Worker) Run(ctx context.Context, interval time.Duration) error {
	if err := w.validate(); err != nil {
		return err
	}
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

func (w *Worker) validate() error {
	if w == nil || w.store == nil || w.reservations == nil || w.settler == nil {
		return errors.New("payment recovery worker is unavailable")
	}
	return w.config.Validate()
}

func (w *Worker) reconcile(ctx context.Context, attempt Attempt) error {
	getCtx, cancel := context.WithTimeout(ctx, w.config.CallTimeout)
	reservation, err := w.reservations.GetReservation(getCtx, attempt.Payment.ReservationID)
	cancel()
	if errors.Is(err, order.ErrNotFound) {
		return w.reset(ctx, attempt, false)
	}
	if err != nil {
		_ = w.done(ctx, attempt)
		return errors.New("get payment reservation failed")
	}
	if reservation.ReservationID != attempt.Payment.ReservationID || reservation.PaymentAttemptID != "" && reservation.PaymentAttemptID != attempt.Payment.ID {
		_ = w.done(ctx, attempt)
		return errors.New("payment reservation identity mismatch")
	}
	switch reservation.Status {
	case order.ReservationReserved:
		settleCtx, settleCancel := context.WithTimeout(ctx, w.config.CallTimeout)
		err = w.settler.SettleRecovered(settleCtx, attempt)
		settleCancel()
		if errors.Is(err, order.ErrInsufficientBalance) {
			return w.reset(ctx, attempt, true)
		}
		if err != nil {
			_ = w.done(ctx, attempt)
			return errors.New("resume wallet settlement failed")
		}
	case order.ReservationConfirmed:
		// A confirmed reservation implies a prior PAID settlement; leave the order untouched.
	case order.ReservationReleased:
		return w.reset(ctx, attempt, false)
	default:
		_ = w.done(ctx, attempt)
		return errors.New("unknown payment reservation state")
	}
	return w.done(ctx, attempt)
}

func (w *Worker) reset(ctx context.Context, attempt Attempt, release bool) error {
	resetCtx, cancel := context.WithTimeout(ctx, w.config.CallTimeout)
	changed, err := w.store.Reset(resetCtx, attempt)
	cancel()
	if err != nil || !changed {
		return errors.New("reset payment recovery claim failed")
	}
	if release {
		releaseCtx, releaseCancel := context.WithTimeout(ctx, w.config.CallTimeout)
		_ = w.reservations.ReleaseReservation(releaseCtx, attempt.Payment.ReservationID)
		releaseCancel()
	}
	return w.done(ctx, attempt)
}

func (w *Worker) done(ctx context.Context, attempt Attempt) error {
	doneCtx, cancel := context.WithTimeout(ctx, w.config.CallTimeout)
	err := w.store.Done(doneCtx, attempt, time.Now().UTC().Truncate(time.Millisecond))
	cancel()
	return err
}
