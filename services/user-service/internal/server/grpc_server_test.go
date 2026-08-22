package server

import (
	"context"
	"database/sql"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	platformauth "github.com/yuyu945/AI-Shopping/internal/platform/auth"
	userpb "github.com/yuyu945/AI-Shopping/services/user-service/gen"
	"github.com/yuyu945/AI-Shopping/services/user-service/internal/user"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const grpcTestSecret = "01234567890123456789012345678901"

func TestGetMyAddressDerivesOwnerFromBearer(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	manager := newServerTestManager(t)
	token, _, err := manager.Issue(platformauth.Principal{UserID: 7, Email: "ada@example.com"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id, receiver_name, receiver_phone, province, city, district, detail, is_default FROM user_addresses WHERE id = ? AND user_id = ?")).WithArgs(uint64(9), uint64(7)).WillReturnRows(sqlmock.NewRows([]string{"id", "receiver_name", "receiver_phone", "province", "city", "district", "detail", "is_default"}).AddRow(uint64(9), "Ada", "13800000000", "Beijing", "Beijing", "Haidian", "Road 1", true))
	server := NewGRPCServer(user.NewUserService(user.NewRepository(db), nil, manager, time.Now), manager, time.Second)
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer "+token))
	got, err := server.GetMyAddress(ctx, &userpb.GetMyAddressRequest{AddressId: 9})
	if err != nil || got.GetAddress().GetAddressId() != 9 {
		t.Fatalf("GetMyAddress() = %#v, %v", got, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestGetMyAddressMapsForeignOrMissingAddressToNotFound(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	manager := newServerTestManager(t)
	token, _, err := manager.Issue(platformauth.Principal{UserID: 7, Email: "ada@example.com"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id, receiver_name, receiver_phone, province, city, district, detail, is_default FROM user_addresses WHERE id = ? AND user_id = ?")).WithArgs(uint64(9), uint64(7)).WillReturnError(sql.ErrNoRows)
	server := NewGRPCServer(user.NewUserService(user.NewRepository(db), nil, manager, time.Now), manager, time.Second)
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer "+token))
	_, err = server.GetMyAddress(ctx, &userpb.GetMyAddressRequest{AddressId: 9})
	if status.Code(err) != codes.NotFound || status.Convert(err).Message() != "resource not found" {
		t.Fatalf("GetMyAddress() error = %v", err)
	}
}

func TestGetMyAddressAppliesDeadline(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	manager := newServerTestManager(t)
	token, _, err := manager.Issue(platformauth.Principal{UserID: 7, Email: "ada@example.com"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id, receiver_name, receiver_phone, province, city, district, detail, is_default FROM user_addresses WHERE id = ? AND user_id = ?")).WithArgs(uint64(9), uint64(7)).WillDelayFor(50 * time.Millisecond).WillReturnRows(sqlmock.NewRows([]string{"id", "receiver_name", "receiver_phone", "province", "city", "district", "detail", "is_default"}))
	server := NewGRPCServer(user.NewUserService(user.NewRepository(db), nil, manager, time.Now), manager, time.Millisecond)
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer "+token))
	_, err = server.GetMyAddress(ctx, &userpb.GetMyAddressRequest{AddressId: 9})
	if status.Code(err) != codes.DeadlineExceeded || status.Convert(err).Message() != "dependency timeout" {
		t.Fatalf("GetMyAddress() error = %v", err)
	}
}

func newServerTestManager(t *testing.T) *platformauth.Manager {
	t.Helper()
	manager, err := platformauth.NewManager([]byte(grpcTestSecret))
	if err != nil {
		t.Fatal(err)
	}
	return manager
}
