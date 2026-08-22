package catalog

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/yuyu945/AI-Shopping/internal/platform/apperror"
)

var (
	// ErrReservationNotFound identifies an unknown reservation ID.
	ErrReservationNotFound = errors.New("reservation not found")
	// ErrReservationOutOfStock identifies a conditional inventory update that did not apply.
	ErrReservationOutOfStock = errors.New("reservation inventory unavailable")
	// ErrReservationConflict identifies a replay whose durable payload differs.
	ErrReservationConflict = errors.New("reservation idempotency conflict")
	// ErrReservationState identifies an invalid durable reservation state transition.
	ErrReservationState = errors.New("reservation state transition rejected")
)

// ReservationStatus is the durable state of an inventory reservation group.
type ReservationStatus string

const (
	ReservationReserved  ReservationStatus = "RESERVED"
	ReservationConfirmed ReservationStatus = "CONFIRMED"
	ReservationReleased  ReservationStatus = "RELEASED"
)

// ReservationItem identifies one SKU quantity held by a reservation.
type ReservationItem struct {
	SKUID    uint64
	Quantity uint32
}

// ReserveStockInput contains only IDs persisted by order-service and SKU quantities.
type ReserveStockInput struct {
	ReservationID    string
	OrderNo          string
	PaymentAttemptID string
	Items            []ReservationItem
	ExpiresAt        time.Time
}

// Reservation is the deterministic aggregate response for one reservation ID.
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

// ReservationMutation records cache keys durably scheduled by an inventory-changing transition.
type ReservationMutation struct {
	CacheKeys []string
	TaskIDs   []uint64
}

// ReservationStore persists catalog-owned reservations and inventory changes.
type ReservationStore interface {
	ReserveStock(context.Context, ReserveStockInput, time.Time, time.Time) (Reservation, ReservationMutation, error)
	ConfirmReservation(context.Context, string, time.Time) (Reservation, error)
	ReleaseReservation(context.Context, string, time.Time, time.Time) (Reservation, ReservationMutation, error)
	GetReservation(context.Context, string) (Reservation, error)
}

// ReservationService coordinates validated reservations with committed cache invalidation.
type ReservationService struct {
	store              ReservationStore
	cache              DetailCache
	now                func() time.Time
	delayedDeleteDelay time.Duration
	cacheCallTimeout   time.Duration
}

// NewReservationService constructs a reservation application service.
func NewReservationService(store ReservationStore, cache DetailCache, now func() time.Time, delayedDeleteDelay, cacheCallTimeout time.Duration) (*ReservationService, error) {
	if store == nil {
		return nil, errors.New("reservation store is required")
	}
	if delayedDeleteDelay <= 0 {
		return nil, apperror.New(apperror.InvalidArgument, "delayed_delete_delay must be positive")
	}
	if cacheCallTimeout <= 0 {
		return nil, apperror.New(apperror.InvalidArgument, "cache_call_timeout must be positive")
	}
	if now == nil {
		now = time.Now
	}
	return &ReservationService{store: store, cache: cache, now: now, delayedDeleteDelay: delayedDeleteDelay, cacheCallTimeout: cacheCallTimeout}, nil
}

// ReserveStock atomically holds every requested SKU or holds none.
func (s *ReservationService) ReserveStock(ctx context.Context, input ReserveStockInput) (Reservation, error) {
	now := normalizeMySQLTimestamp(s.now())
	input, err := normalizeReserveInput(input, now)
	if err != nil {
		return Reservation{}, err
	}
	reservation, mutation, err := s.store.ReserveStock(ctx, input, now, now.Add(s.delayedDeleteDelay))
	if err != nil {
		return Reservation{}, safeReservationError(ctx, "reserve inventory", err)
	}
	s.deleteCommittedCacheKeys(ctx, mutation.CacheKeys)
	return cloneReservation(reservation), nil
}

// ConfirmReservation finalizes a reserved group and accepts an already-confirmed replay.
func (s *ReservationService) ConfirmReservation(ctx context.Context, reservationID string) (Reservation, error) {
	if err := validateReservationID(reservationID); err != nil {
		return Reservation{}, err
	}
	reservation, err := s.store.ConfirmReservation(ctx, strings.TrimSpace(reservationID), normalizeMySQLTimestamp(s.now()))
	if err != nil {
		return Reservation{}, safeReservationError(ctx, "confirm inventory reservation", err)
	}
	return cloneReservation(reservation), nil
}

// ReleaseReservation restores each held quantity exactly once and accepts an already-released replay.
func (s *ReservationService) ReleaseReservation(ctx context.Context, reservationID string) (Reservation, error) {
	if err := validateReservationID(reservationID); err != nil {
		return Reservation{}, err
	}
	now := normalizeMySQLTimestamp(s.now())
	reservation, mutation, err := s.store.ReleaseReservation(ctx, strings.TrimSpace(reservationID), now, now.Add(s.delayedDeleteDelay))
	if err != nil {
		return Reservation{}, safeReservationError(ctx, "release inventory reservation", err)
	}
	s.deleteCommittedCacheKeys(ctx, mutation.CacheKeys)
	return cloneReservation(reservation), nil
}

// GetReservation returns the complete group in SKU order.
func (s *ReservationService) GetReservation(ctx context.Context, reservationID string) (Reservation, error) {
	if err := validateReservationID(reservationID); err != nil {
		return Reservation{}, err
	}
	reservation, err := s.store.GetReservation(ctx, strings.TrimSpace(reservationID))
	if err != nil {
		return Reservation{}, safeReservationError(ctx, "get inventory reservation", err)
	}
	return cloneReservation(reservation), nil
}

func (s *ReservationService) deleteCommittedCacheKeys(ctx context.Context, keys []string) {
	if s.cache == nil {
		return
	}
	for _, key := range keys {
		deleteCtx, cancel := context.WithTimeout(ctx, s.cacheCallTimeout)
		_ = s.cache.Delete(deleteCtx, key)
		cancel()
	}
}

func normalizeReserveInput(input ReserveStockInput, now time.Time) (ReserveStockInput, error) {
	if err := validateReservationID(input.ReservationID); err != nil {
		return ReserveStockInput{}, err
	}
	input.ReservationID = strings.TrimSpace(input.ReservationID)
	input.OrderNo = strings.TrimSpace(input.OrderNo)
	input.PaymentAttemptID = strings.TrimSpace(input.PaymentAttemptID)
	if input.OrderNo == "" || len(input.OrderNo) > 64 || input.PaymentAttemptID == "" || len(input.PaymentAttemptID) > 36 {
		return ReserveStockInput{}, apperror.New(apperror.InvalidArgument, "order_no and payment_attempt_id are required")
	}
	if len(input.Items) == 0 || len(input.Items) > 100 {
		return ReserveStockInput{}, apperror.New(apperror.InvalidArgument, "items must contain between 1 and 100 values")
	}
	input.ExpiresAt = normalizeMySQLTimestamp(input.ExpiresAt)
	if input.ExpiresAt.IsZero() || !input.ExpiresAt.After(now) {
		return ReserveStockInput{}, apperror.New(apperror.InvalidArgument, "expires_at must be in the future")
	}
	items := append([]ReservationItem(nil), input.Items...)
	sort.Slice(items, func(i, j int) bool { return items[i].SKUID < items[j].SKUID })
	for i, item := range items {
		if item.SKUID == 0 || item.Quantity == 0 {
			return ReserveStockInput{}, apperror.New(apperror.InvalidArgument, "sku_id and quantity must be positive")
		}
		if i > 0 && items[i-1].SKUID == item.SKUID {
			return ReserveStockInput{}, apperror.New(apperror.InvalidArgument, "sku_ids must be unique")
		}
	}
	input.Items = items
	return input, nil
}

func validateReservationID(value string) error {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 36 {
		return apperror.New(apperror.InvalidArgument, "reservation_id is required")
	}
	return nil
}

func safeReservationError(ctx context.Context, operation string, err error) error {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return apperror.Wrap(apperror.DependencyTimeout, "catalog dependency timed out", err)
	}
	switch {
	case errors.Is(err, ErrReservationNotFound):
		return apperror.Wrap(apperror.NotFound, "inventory reservation not found", err)
	case errors.Is(err, ErrReservationOutOfStock):
		return apperror.Wrap(apperror.OutOfStock, "requested inventory is unavailable", err)
	case errors.Is(err, ErrReservationConflict), errors.Is(err, ErrReservationState):
		return apperror.Wrap(apperror.IdempotencyConflict, "inventory reservation cannot be changed", err)
	default:
		return apperror.Wrap(apperror.Internal, operation+" failed", err)
	}
}

func cloneReservation(value Reservation) Reservation {
	result := value
	result.Items = append([]ReservationItem(nil), value.Items...)
	if value.ConfirmedAt != nil {
		at := *value.ConfirmedAt
		result.ConfirmedAt = &at
	}
	if value.ReleasedAt != nil {
		at := *value.ReleasedAt
		result.ReleasedAt = &at
	}
	return result
}
