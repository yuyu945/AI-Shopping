package server

import (
	"context"
	"testing"
	"time"

	productpb "github.com/yuyu945/AI-Shopping/services/product-service/gen"
	"github.com/yuyu945/AI-Shopping/services/product-service/internal/catalog"
	"google.golang.org/grpc/codes"
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
	server := NewGRPCServerWithReservations(catalog.NewProductService(checkoutRepository{}, nil), reservations, time.Second)
	got, err := server.ReserveStock(context.Background(), &productpb.ReserveStockRequest{ReservationId: "r-1", OrderNo: "o-1", PaymentAttemptId: "p-1", ExpiresAtUnixMilli: now.Add(time.Minute).UnixMilli(), Items: []*productpb.ReservationItem{{SkuId: 7, Quantity: 2}}})
	if err != nil || got.GetReservation().GetStatus() != "RESERVED" || got.GetReservation().GetItems()[0].GetSkuId() != 7 {
		t.Fatalf("ReserveStock()=%#v err=%v", got, err)
	}
}

func TestGRPCServerReservationRejectsMissingInput(t *testing.T) {
	server := NewGRPCServer(catalog.NewProductService(checkoutRepository{}, nil), time.Second)
	got, err := server.ReserveStock(context.Background(), nil)
	if got != nil || status.Code(err) != codes.InvalidArgument {
		t.Fatalf("ReserveStock(nil)=%#v err=%v", got, err)
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
