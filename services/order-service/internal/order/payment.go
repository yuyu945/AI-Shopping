package order

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/yuyu945/AI-Shopping/internal/platform/apperror"
)

var (
	// ErrPaymentInProgress indicates that another caller owns the durable payment claim.
	ErrPaymentInProgress = errors.New("payment in progress")
	// ErrInsufficientBalance indicates that the locked wallet cannot cover the order total.
	ErrInsufficientBalance = errors.New("insufficient wallet balance")
)

// ReservationStatus is the product-owned state returned for a reservation group.
type ReservationStatus string

const (
	ReservationReserved  ReservationStatus = "RESERVED"
	ReservationConfirmed ReservationStatus = "CONFIRMED"
	ReservationReleased  ReservationStatus = "RELEASED"
)

// ReservationItem is an immutable SKU quantity copied from an order snapshot.
type ReservationItem struct {
	SKUID    uint64
	Quantity uint32
}

// ReserveRequest is sent to product-service only after the short claim transaction commits.
type ReserveRequest struct {
	ReservationID    string
	OrderNo          string
	PaymentAttemptID string
	Items            []ReservationItem
	ExpiresAt        time.Time
}

// Reservation is the minimal product-owned response required for local settlement.
type Reservation struct {
	ReservationID    string
	OrderNo          string
	PaymentAttemptID string
	Status           ReservationStatus
	ExpiresAt        time.Time
	ConfirmedAt      *time.Time
	ReleasedAt       *time.Time
	Items            []ReservationItem
}

// ReservationClient is an adapter boundary; its gRPC implementation belongs to the transport task.
type ReservationClient interface {
	ReserveStock(context.Context, ReserveRequest) (Reservation, error)
	ReleaseReservation(context.Context, string) error
	GetReservation(context.Context, string) (Reservation, error)
}

// PaymentRepository owns all order-service database transitions used by wallet payment.
type PaymentRepository interface {
	ClaimPayment(context.Context, uint64, string, PaymentAttempt) (Order, error)
	ResetPaymentClaim(context.Context, uint64, string, PaymentAttempt) (bool, error)
	SettleWalletPayment(context.Context, uint64, string, PaymentAttempt) (Order, error)
}

// IDGenerator produces opaque identifiers for durable payment attempts and reservations.
type IDGenerator interface {
	New() string
}

// IDGeneratorFunc adapts a function for focused tests and simple runtime wiring.
type IDGeneratorFunc func() string

// New returns the next opaque identifier.
func (f IDGeneratorFunc) New() string { return f() }

// PaymentService coordinates the three Saga phases without holding a trade transaction during RPC.
type PaymentService struct {
	repository     PaymentRepository
	reservations   ReservationClient
	ids            IDGenerator
	reservationTTL time.Duration
}

// NewPaymentService creates the order-side payment coordinator.
func NewPaymentService(repository PaymentRepository, reservations ReservationClient, ids IDGenerator, reservationTTL time.Duration) *PaymentService {
	if ids == nil {
		ids = IDGeneratorFunc(func() string { return "" })
	}
	return &PaymentService{repository: repository, reservations: reservations, ids: ids, reservationTTL: reservationTTL}
}

// PayWallet claims an order, reserves immutable SKU quantities, then settles entirely in trade_db.
func (s *PaymentService) PayWallet(ctx context.Context, userID uint64, orderNo string) (Order, error) {
	if userID == 0 || strings.TrimSpace(orderNo) == "" {
		return Order{}, invalid("order number is required")
	}
	if s.repository == nil || s.reservations == nil || s.reservationTTL <= 0 {
		return Order{}, &Error{Code: Internal, Message: "payment service is unavailable"}
	}
	attempt := PaymentAttempt{ID: s.ids.New(), ReservationID: s.ids.New()}
	if attempt.ID == "" || attempt.ReservationID == "" {
		return Order{}, &Error{Code: Internal, Message: "payment service is unavailable"}
	}
	order, err := s.repository.ClaimPayment(ctx, userID, strings.TrimSpace(orderNo), attempt)
	if err != nil {
		return Order{}, paymentRepositoryError(err)
	}
	if order.Status == Paid {
		return order, nil
	}
	if order.Status != PaymentProcessing || order.Payment != attempt {
		return Order{}, &Error{Code: PaymentInProgress, Message: "payment is in progress"}
	}
	request := ReserveRequest{ReservationID: attempt.ReservationID, OrderNo: order.OrderNo, PaymentAttemptID: attempt.ID, ExpiresAt: time.Now().Add(s.reservationTTL)}
	request.Items = make([]ReservationItem, 0, len(order.Items))
	for _, item := range order.Items {
		request.Items = append(request.Items, ReservationItem{SKUID: item.SKUID, Quantity: item.Quantity})
	}
	reservation, err := s.reservations.ReserveStock(ctx, request)
	if err != nil {
		if isOutOfStock(err) {
			reset, resetErr := s.repository.ResetPaymentClaim(ctx, userID, order.OrderNo, attempt)
			if resetErr != nil {
				return Order{}, paymentRepositoryError(resetErr)
			}
			if !reset {
				return Order{}, &Error{Code: PaymentInProgress, Message: "payment is in progress"}
			}
			return Order{}, &Error{Code: OutOfStock, Message: "requested inventory is unavailable"}
		}
		return Order{}, paymentDependencyError(ctx, err)
	}
	if reservation.ReservationID != attempt.ReservationID || reservation.Status != ReservationReserved {
		return Order{}, &Error{Code: Internal, Message: "inventory reservation is unavailable"}
	}
	settled, err := s.repository.SettleWalletPayment(ctx, userID, order.OrderNo, attempt)
	if err == nil {
		return settled, nil
	}
	if errors.Is(err, ErrInsufficientBalance) {
		reset, resetErr := s.repository.ResetPaymentClaim(ctx, userID, order.OrderNo, attempt)
		if resetErr != nil {
			return Order{}, paymentRepositoryError(resetErr)
		}
		if !reset {
			return Order{}, &Error{Code: PaymentInProgress, Message: "payment is in progress"}
		}
		_ = s.reservations.ReleaseReservation(ctx, attempt.ReservationID)
		return Order{}, &Error{Code: InsufficientBalance, Message: "wallet balance is insufficient"}
	}
	return Order{}, paymentRepositoryError(err)
}

func isOutOfStock(err error) bool {
	var appErr *apperror.Error
	return errors.As(err, &appErr) && appErr.Code == apperror.OutOfStock
}

func paymentRepositoryError(err error) error {
	switch {
	case errors.Is(err, ErrPaymentInProgress):
		return &Error{Code: PaymentInProgress, Message: "payment is in progress"}
	case errors.Is(err, ErrInsufficientBalance):
		return &Error{Code: InsufficientBalance, Message: "wallet balance is insufficient"}
	case errors.Is(err, ErrNotFound):
		return &Error{Code: NotFound, Message: "resource not found"}
	default:
		return repositoryError(err)
	}
}

func paymentDependencyError(ctx context.Context, err error) error {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return &Error{Code: DependencyTimeout, Message: "dependency timeout"}
	}
	return &Error{Code: Internal, Message: "dependency unavailable"}
}
