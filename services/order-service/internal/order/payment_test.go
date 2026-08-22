package order

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/yuyu945/AI-Shopping/internal/platform/apperror"
)

type fakePaymentRepository struct {
	claim  func(context.Context, uint64, string, PaymentAttempt) (Order, error)
	reset  func(context.Context, uint64, string, PaymentAttempt) (bool, error)
	settle func(context.Context, uint64, string, PaymentAttempt) (Order, error)
}

func (r fakePaymentRepository) ClaimPayment(ctx context.Context, userID uint64, orderNo string, attempt PaymentAttempt) (Order, error) {
	return r.claim(ctx, userID, orderNo, attempt)
}
func (r fakePaymentRepository) ResetPaymentClaim(ctx context.Context, userID uint64, orderNo string, attempt PaymentAttempt) (bool, error) {
	return r.reset(ctx, userID, orderNo, attempt)
}
func (r fakePaymentRepository) SettleWalletPayment(ctx context.Context, userID uint64, orderNo string, attempt PaymentAttempt) (Order, error) {
	return r.settle(ctx, userID, orderNo, attempt)
}

type fakeReservations struct {
	reserve func(context.Context, ReserveRequest) (Reservation, error)
	release func(context.Context, string) error
	get     func(context.Context, string) (Reservation, error)
}

func (r fakeReservations) ReserveStock(ctx context.Context, request ReserveRequest) (Reservation, error) {
	return r.reserve(ctx, request)
}
func (r fakeReservations) ReleaseReservation(ctx context.Context, reservationID string) error {
	return r.release(ctx, reservationID)
}
func (r fakeReservations) GetReservation(ctx context.Context, reservationID string) (Reservation, error) {
	if r.get == nil {
		return Reservation{}, errors.New("get reservation not configured")
	}
	return r.get(ctx, reservationID)
}

func TestPayWalletReturnsPreviouslyPaidOrderWithoutReservingAgain(t *testing.T) {
	paid := Order{OrderNo: "order-1", Status: Paid, PaidAmount: "12.00"}
	reservationsCalled := false
	service := NewPaymentService(fakePaymentRepository{
		claim: func(context.Context, uint64, string, PaymentAttempt) (Order, error) { return paid, nil },
		reset: func(context.Context, uint64, string, PaymentAttempt) (bool, error) {
			t.Fatal("ResetPaymentClaim must not be called")
			return false, nil
		},
		settle: func(context.Context, uint64, string, PaymentAttempt) (Order, error) {
			t.Fatal("SettleWalletPayment must not be called")
			return Order{}, nil
		},
	}, fakeReservations{reserve: func(context.Context, ReserveRequest) (Reservation, error) {
		reservationsCalled = true
		return Reservation{}, nil
	}, release: func(context.Context, string) error { return nil }}, fixedPaymentIDs(), time.Minute)

	got, err := service.PayWallet(context.Background(), 7, "order-1")
	if err != nil || got.OrderNo != paid.OrderNo || got.Status != Paid || got.PaidAmount != paid.PaidAmount || reservationsCalled {
		t.Fatalf("PayWallet() = %#v, %v, reserve=%v", got, err, reservationsCalled)
	}
}

func TestPayWalletResetsMatchingClaimAfterOutOfStock(t *testing.T) {
	attempt := PaymentAttempt{ID: "attempt-1", ReservationID: "reservation-1"}
	claimed := Order{OrderNo: "order-1", Status: PaymentProcessing, Payment: attempt, Items: []OrderItem{{SKUID: 101, Quantity: 2}}}
	reset := false
	service := NewPaymentService(fakePaymentRepository{
		claim: func(_ context.Context, _ uint64, _ string, got PaymentAttempt) (Order, error) {
			if got != attempt {
				t.Fatalf("attempt = %#v", got)
			}
			return claimed, nil
		},
		reset: func(_ context.Context, _ uint64, _ string, got PaymentAttempt) (bool, error) {
			reset = got == attempt
			return true, nil
		},
		settle: func(context.Context, uint64, string, PaymentAttempt) (Order, error) {
			t.Fatal("SettleWalletPayment must not be called")
			return Order{}, nil
		},
	}, fakeReservations{reserve: func(_ context.Context, request ReserveRequest) (Reservation, error) {
		if request.ReservationID != attempt.ReservationID || request.Items[0] != (ReservationItem{SKUID: 101, Quantity: 2}) {
			t.Fatalf("reserve request = %#v", request)
		}
		return Reservation{}, apperror.New(apperror.OutOfStock, "requested inventory is unavailable")
	}, release: func(context.Context, string) error {
		t.Fatal("release must not be called when reserve failed atomically")
		return nil
	}}, fixedPaymentIDs(), time.Minute)

	_, err := service.PayWallet(context.Background(), 7, "order-1")
	if !IsCode(err, OutOfStock) || !reset {
		t.Fatalf("PayWallet error = %v, reset=%v", err, reset)
	}
}

func TestPayWalletPreservesConcurrentSettlementAfterOutOfStock(t *testing.T) {
	attempt := PaymentAttempt{ID: "attempt-1", ReservationID: "reservation-1"}
	service := NewPaymentService(fakePaymentRepository{
		claim: func(context.Context, uint64, string, PaymentAttempt) (Order, error) {
			return Order{OrderNo: "order-1", Status: PaymentProcessing, Payment: attempt, Items: []OrderItem{{SKUID: 101, Quantity: 2}}}, nil
		},
		reset: func(context.Context, uint64, string, PaymentAttempt) (bool, error) {
			return false, nil // Another caller has already settled the matching claim.
		},
		settle: func(context.Context, uint64, string, PaymentAttempt) (Order, error) {
			t.Fatal("SettleWalletPayment must not be called")
			return Order{}, nil
		},
	}, fakeReservations{reserve: func(context.Context, ReserveRequest) (Reservation, error) {
		return Reservation{}, apperror.New(apperror.OutOfStock, "requested inventory is unavailable")
	}, release: func(context.Context, string) error {
		t.Fatal("release must not be called when reserve failed atomically")
		return nil
	}}, fixedPaymentIDs(), time.Minute)

	_, err := service.PayWallet(context.Background(), 7, "order-1")
	if !IsCode(err, PaymentInProgress) {
		t.Fatalf("PayWallet error = %v", err)
	}
}

func TestPayWalletPreservesProcessingAttemptWhenReservationTimesOut(t *testing.T) {
	attempt := PaymentAttempt{ID: "attempt-1", ReservationID: "reservation-1"}
	reset := false
	service := NewPaymentService(fakePaymentRepository{
		claim: func(context.Context, uint64, string, PaymentAttempt) (Order, error) {
			return Order{OrderNo: "order-1", Status: PaymentProcessing, Payment: attempt, Items: []OrderItem{{SKUID: 101, Quantity: 2}}}, nil
		},
		reset: func(context.Context, uint64, string, PaymentAttempt) (bool, error) { reset = true; return true, nil },
		settle: func(context.Context, uint64, string, PaymentAttempt) (Order, error) {
			t.Fatal("SettleWalletPayment must not be called")
			return Order{}, nil
		},
	}, fakeReservations{reserve: func(context.Context, ReserveRequest) (Reservation, error) {
		return Reservation{}, context.DeadlineExceeded
	}, release: func(context.Context, string) error { return nil }}, fixedPaymentIDs(), time.Minute)

	_, err := service.PayWallet(context.Background(), 7, "order-1")
	if !IsCode(err, DependencyTimeout) || reset {
		t.Fatalf("PayWallet error = %v, reset=%v", err, reset)
	}
}

func TestPayWalletReleasesReservationAfterInsufficientBalance(t *testing.T) {
	attempt := PaymentAttempt{ID: "attempt-1", ReservationID: "reservation-1"}
	reset, released := false, false
	service := NewPaymentService(fakePaymentRepository{
		claim: func(context.Context, uint64, string, PaymentAttempt) (Order, error) {
			return Order{OrderNo: "order-1", Status: PaymentProcessing, Payment: attempt, Items: []OrderItem{{SKUID: 101, Quantity: 2}}}, nil
		},
		reset: func(_ context.Context, _ uint64, _ string, got PaymentAttempt) (bool, error) {
			reset = got == attempt
			return true, nil
		},
		settle: func(context.Context, uint64, string, PaymentAttempt) (Order, error) {
			return Order{}, ErrInsufficientBalance
		},
	}, fakeReservations{reserve: func(context.Context, ReserveRequest) (Reservation, error) {
		return Reservation{ReservationID: attempt.ReservationID, Status: ReservationReserved}, nil
	}, release: func(_ context.Context, got string) error {
		released = got == attempt.ReservationID
		return errors.New("ignored release failure")
	}}, fixedPaymentIDs(), time.Minute)

	_, err := service.PayWallet(context.Background(), 7, "order-1")
	if !IsCode(err, InsufficientBalance) || !reset || !released {
		t.Fatalf("PayWallet error = %v, reset=%v release=%v", err, reset, released)
	}
}

func TestPayWalletDoesNotReleaseReservationWhenBalanceResetLosesClaim(t *testing.T) {
	attempt := PaymentAttempt{ID: "attempt-1", ReservationID: "reservation-1"}
	released := false
	service := NewPaymentService(fakePaymentRepository{
		claim: func(context.Context, uint64, string, PaymentAttempt) (Order, error) {
			return Order{OrderNo: "order-1", Status: PaymentProcessing, Payment: attempt, Items: []OrderItem{{SKUID: 101, Quantity: 2}}}, nil
		},
		reset: func(context.Context, uint64, string, PaymentAttempt) (bool, error) {
			return false, nil // A concurrent settlement has already moved this exact claim to PAID.
		},
		settle: func(context.Context, uint64, string, PaymentAttempt) (Order, error) {
			return Order{}, ErrInsufficientBalance
		},
	}, fakeReservations{reserve: func(context.Context, ReserveRequest) (Reservation, error) {
		return Reservation{ReservationID: attempt.ReservationID, Status: ReservationReserved}, nil
	}, release: func(context.Context, string) error {
		released = true
		return nil
	}}, fixedPaymentIDs(), time.Minute)

	_, err := service.PayWallet(context.Background(), 7, "order-1")
	if !IsCode(err, PaymentInProgress) || released {
		t.Fatalf("PayWallet error = %v, released=%v", err, released)
	}
}

func TestPayWalletDoesNotReleaseReservationWhenBalanceResetFails(t *testing.T) {
	attempt := PaymentAttempt{ID: "attempt-1", ReservationID: "reservation-1"}
	released := false
	service := NewPaymentService(fakePaymentRepository{
		claim: func(context.Context, uint64, string, PaymentAttempt) (Order, error) {
			return Order{OrderNo: "order-1", Status: PaymentProcessing, Payment: attempt, Items: []OrderItem{{SKUID: 101, Quantity: 2}}}, nil
		},
		reset: func(context.Context, uint64, string, PaymentAttempt) (bool, error) {
			return false, errors.New("database unavailable")
		},
		settle: func(context.Context, uint64, string, PaymentAttempt) (Order, error) {
			return Order{}, ErrInsufficientBalance
		},
	}, fakeReservations{reserve: func(context.Context, ReserveRequest) (Reservation, error) {
		return Reservation{ReservationID: attempt.ReservationID, Status: ReservationReserved}, nil
	}, release: func(context.Context, string) error {
		released = true
		return nil
	}}, fixedPaymentIDs(), time.Minute)

	_, err := service.PayWallet(context.Background(), 7, "order-1")
	if !IsCode(err, Internal) || released {
		t.Fatalf("PayWallet error = %v, released=%v", err, released)
	}
}

func TestPayWalletDoesNotSettleUnexpectedReservationResponse(t *testing.T) {
	attempt := PaymentAttempt{ID: "attempt-1", ReservationID: "reservation-1"}
	settled := false
	service := NewPaymentService(fakePaymentRepository{
		claim: func(context.Context, uint64, string, PaymentAttempt) (Order, error) {
			return Order{OrderNo: "order-1", Status: PaymentProcessing, Payment: attempt, Items: []OrderItem{{SKUID: 101, Quantity: 1}}}, nil
		},
		reset: func(context.Context, uint64, string, PaymentAttempt) (bool, error) {
			t.Fatal("unexpected response must remain recoverable")
			return false, nil
		},
		settle: func(context.Context, uint64, string, PaymentAttempt) (Order, error) {
			settled = true
			return Order{}, nil
		},
	}, fakeReservations{reserve: func(context.Context, ReserveRequest) (Reservation, error) {
		return Reservation{ReservationID: "another-reservation", Status: ReservationReserved}, nil
	}, release: func(context.Context, string) error { return nil }}, fixedPaymentIDs(), time.Minute)

	_, err := service.PayWallet(context.Background(), 7, "order-1")
	if !IsCode(err, Internal) || settled {
		t.Fatalf("PayWallet error = %v, settled=%v", err, settled)
	}
}

func fixedPaymentIDs() IDGenerator {
	values := []string{"attempt-1", "reservation-1"}
	return IDGeneratorFunc(func() string { value := values[0]; values = values[1:]; return value })
}
