package server

import (
	"context"
	"time"

	productpb "github.com/yuyu945/AI-Shopping/services/product-service/gen"
	"github.com/yuyu945/AI-Shopping/services/product-service/internal/catalog"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ReserveStock holds all requested SKU quantities in one catalog transaction.
func (s *GRPCServer) ReserveStock(ctx context.Context, req *productpb.ReserveStockRequest) (*productpb.ReservationResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	service, err := s.reservationService()
	if err != nil {
		return nil, err
	}
	items := make([]catalog.ReservationItem, 0, len(req.GetItems()))
	for _, item := range req.GetItems() {
		if item == nil {
			return nil, status.Error(codes.InvalidArgument, "items must not contain null values")
		}
		items = append(items, catalog.ReservationItem{SKUID: item.GetSkuId(), Quantity: item.GetQuantity()})
	}
	callCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	reservation, err := service.ReserveStock(callCtx, catalog.ReserveStockInput{
		ReservationID: req.GetReservationId(),
		OrderNo:       req.GetOrderNo(), PaymentAttemptID: req.GetPaymentAttemptId(),
		Items: items, ExpiresAt: time.UnixMilli(req.GetExpiresAtUnixMilli()).UTC(),
	})
	if err != nil {
		return nil, toStatusError(err)
	}
	return &productpb.ReservationResponse{Reservation: mapReservation(reservation)}, nil
}

// ConfirmReservation completes a held reservation idempotently.
func (s *GRPCServer) ConfirmReservation(ctx context.Context, req *productpb.ReservationActionRequest) (*productpb.ReservationResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	service, err := s.reservationService()
	if err != nil {
		return nil, err
	}
	callCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	reservation, err := service.ConfirmReservation(callCtx, req.GetReservationId())
	if err != nil {
		return nil, toStatusError(err)
	}
	return &productpb.ReservationResponse{Reservation: mapReservation(reservation)}, nil
}

// ReleaseReservation restores an unconfirmed reservation idempotently.
func (s *GRPCServer) ReleaseReservation(ctx context.Context, req *productpb.ReservationActionRequest) (*productpb.ReservationResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	service, err := s.reservationService()
	if err != nil {
		return nil, err
	}
	callCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	reservation, err := service.ReleaseReservation(callCtx, req.GetReservationId())
	if err != nil {
		return nil, toStatusError(err)
	}
	return &productpb.ReservationResponse{Reservation: mapReservation(reservation)}, nil
}

// GetReservation reads a complete deterministic reservation group.
func (s *GRPCServer) GetReservation(ctx context.Context, req *productpb.GetReservationRequest) (*productpb.ReservationResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	service, err := s.reservationService()
	if err != nil {
		return nil, err
	}
	callCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	reservation, err := service.GetReservation(callCtx, req.GetReservationId())
	if err != nil {
		return nil, toStatusError(err)
	}
	return &productpb.ReservationResponse{Reservation: mapReservation(reservation)}, nil
}

func (s *GRPCServer) reservationService() (*catalog.ReservationService, error) {
	if s.reservations == nil {
		return nil, status.Error(codes.Internal, "internal server error")
	}
	return s.reservations, nil
}

func mapReservation(value catalog.Reservation) *productpb.Reservation {
	result := &productpb.Reservation{
		ReservationId: value.ReservationID, OrderNo: value.OrderNo, PaymentAttemptId: value.PaymentAttemptID,
		Status: string(value.Status), ExpiresAtUnixMilli: value.ExpiresAt.UnixMilli(),
		Items: make([]*productpb.ReservationItem, 0, len(value.Items)),
	}
	if value.ConfirmedAt != nil {
		millis := value.ConfirmedAt.UnixMilli()
		result.ConfirmedAtUnixMilli = &millis
	}
	if value.ReleasedAt != nil {
		millis := value.ReleasedAt.UnixMilli()
		result.ReleasedAtUnixMilli = &millis
	}
	for _, item := range value.Items {
		result.Items = append(result.Items, &productpb.ReservationItem{SkuId: item.SKUID, Quantity: item.Quantity})
	}
	return result
}
