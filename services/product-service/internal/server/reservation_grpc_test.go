package server

import (
	"context"
	"testing"
	"time"

	productpb "github.com/yuyu945/AI-Shopping/services/product-service/gen"
	"github.com/yuyu945/AI-Shopping/services/product-service/internal/catalog"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/reflect/protoreflect"
)

type reservationServerStore struct{ reservation catalog.Reservation }

func (s reservationServerStore) ReserveStock(_ context.Context, input catalog.ReserveStockInput, _, _ time.Time) (catalog.Reservation, catalog.ReservationMutation, error) {
	s.reservation.ReservationID = input.ReservationID
	s.reservation.OrderNo = input.OrderNo
	s.reservation.PaymentAttemptID = input.PaymentAttemptID
	return s.reservation, catalog.ReservationMutation{}, nil
}
func (s reservationServerStore) ConfirmReservation(context.Context, string, time.Time) (catalog.Reservation, error) {
	return s.reservation, nil
}
func (s reservationServerStore) ReleaseReservation(context.Context, string, time.Time, time.Time) (catalog.Reservation, catalog.ReservationMutation, error) {
	return s.reservation, catalog.ReservationMutation{}, nil
}
func (s reservationServerStore) GetReservation(context.Context, string) (catalog.Reservation, error) {
	return s.reservation, nil
}

func TestGRPCServerReserveStockMapsTypedReservation(t *testing.T) {
	now := time.Date(2026, time.August, 22, 9, 0, 0, 0, time.UTC)
	reservations, err := catalog.NewReservationService(reservationServerStore{reservation: catalog.Reservation{Status: catalog.ReservationReserved, ExpiresAt: now.Add(time.Minute), Items: []catalog.ReservationItem{{SKUID: 7, Quantity: 2}}}}, nil, func() time.Time { return now }, time.Second, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	server := NewGRPCServerWithReservations(catalog.NewProductService(checkoutRepository{}, nil), reservations, time.Second, "test-service-token")
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(InternalServiceTokenMetadataKey, "test-service-token"))
	got, err := server.ReserveStock(ctx, &productpb.ReserveStockRequest{ReservationId: "r-1", OrderNo: "o-1", PaymentAttemptId: "p-1", ExpiresAtUnixMilli: now.Add(time.Minute).UnixMilli(), Items: []*productpb.ReservationItem{{SkuId: 7, Quantity: 2}}})
	if err != nil || got.GetReservation().GetStatus() != "RESERVED" || got.GetReservation().GetItems()[0].GetSkuId() != 7 {
		t.Fatalf("ReserveStock()=%#v err=%v", got, err)
	}
}

func TestGRPCServerReservationOperationsRequireServiceToken(t *testing.T) {
	now := time.Date(2026, time.August, 22, 9, 0, 0, 0, time.UTC)
	reservations, err := catalog.NewReservationService(reservationServerStore{reservation: catalog.Reservation{Status: catalog.ReservationReserved, ExpiresAt: now.Add(time.Minute)}}, nil, func() time.Time { return now }, time.Second, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	server := NewGRPCServerWithReservations(catalog.NewProductService(checkoutRepository{}, nil), reservations, time.Second, "test-service-token")

	tests := []struct {
		name  string
		call  func(context.Context) error
		valid func(context.Context) error
	}{
		{name: "reserve", call: func(ctx context.Context) error {
			_, err := server.ReserveStock(ctx, &productpb.ReserveStockRequest{})
			return err
		}, valid: func(ctx context.Context) error {
			_, err := server.ReserveStock(ctx, &productpb.ReserveStockRequest{ReservationId: "r-1", OrderNo: "o-1", PaymentAttemptId: "p-1", ExpiresAtUnixMilli: now.Add(time.Minute).UnixMilli(), Items: []*productpb.ReservationItem{{SkuId: 7, Quantity: 1}}})
			return err
		}},
		{name: "confirm", call: func(ctx context.Context) error {
			_, err := server.ConfirmReservation(ctx, &productpb.ReservationActionRequest{})
			return err
		}, valid: func(ctx context.Context) error {
			_, err := server.ConfirmReservation(ctx, &productpb.ReservationActionRequest{ReservationId: "r-1"})
			return err
		}},
		{name: "release", call: func(ctx context.Context) error {
			_, err := server.ReleaseReservation(ctx, &productpb.ReservationActionRequest{})
			return err
		}, valid: func(ctx context.Context) error {
			_, err := server.ReleaseReservation(ctx, &productpb.ReservationActionRequest{ReservationId: "r-1"})
			return err
		}},
		{name: "get", call: func(ctx context.Context) error {
			_, err := server.GetReservation(ctx, &productpb.GetReservationRequest{})
			return err
		}, valid: func(ctx context.Context) error {
			_, err := server.GetReservation(ctx, &productpb.GetReservationRequest{ReservationId: "r-1"})
			return err
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name+" missing token", func(t *testing.T) {
			err := tt.call(context.Background())
			if status.Code(err) != codes.Unauthenticated || status.Convert(err).Message() != "internal service authentication required" {
				t.Fatalf("error = %v, want unauthenticated stable error", err)
			}
		})
		t.Run(tt.name+" invalid token", func(t *testing.T) {
			ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(InternalServiceTokenMetadataKey, "wrong-token"))
			err := tt.call(ctx)
			if status.Code(err) != codes.Unauthenticated || status.Convert(err).Message() != "internal service authentication required" {
				t.Fatalf("error = %v, want unauthenticated stable error", err)
			}
		})
		t.Run(tt.name+" valid token", func(t *testing.T) {
			ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(InternalServiceTokenMetadataKey, "test-service-token"))
			if err := tt.valid(ctx); err != nil {
				t.Fatalf("valid service token error = %v", err)
			}
		})
	}
}

func TestGRPCServerReservationRejectsMissingInput(t *testing.T) {
	server := NewGRPCServerWithReservations(catalog.NewProductService(checkoutRepository{}, nil), nil, time.Second, "test-service-token")
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(InternalServiceTokenMetadataKey, "test-service-token"))
	got, err := server.ReserveStock(ctx, nil)
	if got != nil || status.Code(err) != codes.InvalidArgument {
		t.Fatalf("ReserveStock(nil)=%#v err=%v", got, err)
	}
}

func TestGRPCServerPublicProductReadsDoNotRequireServiceToken(t *testing.T) {
	server := NewGRPCServerWithReservations(catalog.NewProductService(checkoutRepository{}, nil), nil, time.Second, "test-service-token")
	if _, err := server.ListProducts(context.Background(), &productpb.ListProductsRequest{Page: 1}); err != nil {
		t.Fatalf("ListProducts() error = %v, want public read unaffected", err)
	}
}

func TestProductGRPCContractExposesReservationOperations(t *testing.T) {
	service := productpb.File_api_product_product_proto.Services().ByName("ProductService")
	for _, name := range []string{"ReserveStock", "ConfirmReservation", "ReleaseReservation", "GetReservation"} {
		if service.Methods().ByName(protoreflect.Name(name)) == nil {
			t.Fatalf("ProductService is missing %s", name)
		}
	}
}
