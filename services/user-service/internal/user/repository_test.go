package user

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestRepositoryCreateUserWithProfileIsAtomic(t *testing.T) {
	db, mock := newRepositoryMock(t)
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO users (email, password_hash) VALUES (?, ?)")).
		WithArgs("user@example.com", "hash").
		WillReturnResult(sqlmock.NewResult(42, 1))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO user_profiles (user_id) VALUES (?)")).
		WithArgs(uint64(42)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	got, err := NewRepository(db).CreateUserWithProfile(context.Background(), "user@example.com", "hash")
	if err != nil || got.ID != 42 || got.Email != "user@example.com" {
		t.Fatalf("CreateUserWithProfile() = %#v, %v", got, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRepositoryCreateUserWithProfileRollsBackProfileFailure(t *testing.T) {
	db, mock := newRepositoryMock(t)
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO users (email, password_hash) VALUES (?, ?)")).
		WithArgs("user@example.com", "hash").
		WillReturnResult(sqlmock.NewResult(42, 1))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO user_profiles (user_id) VALUES (?)")).
		WithArgs(uint64(42)).
		WillReturnError(errors.New("profile insert unavailable"))
	mock.ExpectRollback()

	_, err := NewRepository(db).CreateUserWithProfile(context.Background(), "user@example.com", "hash")
	if err == nil {
		t.Fatal("CreateUserWithProfile() error = nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRepositoryCreateFirstAddressMarksItDefault(t *testing.T) {
	db, mock := newRepositoryMock(t)
	input := AddressInput{ReceiverName: "Ada", ReceiverPhone: "13800000000", Province: "Beijing", City: "Beijing", District: "Haidian", Detail: "Road 1"}
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO user_addresses (user_id, receiver_name, receiver_phone, province, city, district, detail, is_default) VALUES (?, ?, ?, ?, ?, ?, ?, FALSE)")).
		WithArgs(uint64(7), input.ReceiverName, input.ReceiverPhone, input.Province, input.City, input.District, input.Detail).
		WillReturnResult(sqlmock.NewResult(9, 1))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT COUNT(*) FROM user_addresses WHERE user_id = ?")).WithArgs(uint64(7)).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE user_addresses SET is_default = TRUE WHERE id = ? AND user_id = ?")).WithArgs(uint64(9), uint64(7)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	got, err := NewRepository(db).CreateAddress(context.Background(), 7, input)
	if err != nil || got.ID != 9 || !got.IsDefault {
		t.Fatalf("CreateAddress() = %#v, %v", got, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRepositoryUpdateAddressHidesNonOwner(t *testing.T) {
	db, mock := newRepositoryMock(t)
	input := AddressInput{ReceiverName: "Ada", ReceiverPhone: "13800000000", Province: "Beijing", City: "Beijing", District: "Haidian", Detail: "Road 1"}
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("UPDATE user_addresses SET receiver_name = ?, receiver_phone = ?, province = ?, city = ?, district = ?, detail = ?, is_default = ? WHERE id = ? AND user_id = ?")).
		WithArgs(input.ReceiverName, input.ReceiverPhone, input.Province, input.City, input.District, input.Detail, false, uint64(9), uint64(7)).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()

	_, err := NewRepository(db).UpdateAddress(context.Background(), 7, 9, input)
	var notFound *NotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("UpdateAddress() error = %v, want NotFoundError", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRepositoryDeleteDefaultPromotesAnotherAddress(t *testing.T) {
	db, mock := newRepositoryMock(t)
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT is_default FROM user_addresses WHERE id = ? AND user_id = ?")).WithArgs(uint64(9), uint64(7)).WillReturnRows(sqlmock.NewRows([]string{"is_default"}).AddRow(true))
	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM user_addresses WHERE id = ? AND user_id = ?")).WithArgs(uint64(9), uint64(7)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE user_addresses SET is_default = TRUE WHERE user_id = ? ORDER BY id ASC LIMIT 1")).WithArgs(uint64(7)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	if err := NewRepository(db).DeleteAddress(context.Background(), 7, 9); err != nil {
		t.Fatalf("DeleteAddress() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRepositoryFindUserAndUpdateProfile(t *testing.T) {
	db, mock := newRepositoryMock(t)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id, email, password_hash FROM users WHERE email = ? AND status = 'ACTIVE'")).WithArgs("user@example.com").WillReturnRows(sqlmock.NewRows([]string{"id", "email", "password_hash"}).AddRow(7, "user@example.com", "hash"))
	got, err := NewRepository(db).FindUserByEmail(context.Background(), "user@example.com")
	if err != nil || got.ID != 7 || got.PasswordHash != "hash" {
		t.Fatalf("FindUserByEmail() = %#v, %v", got, err)
	}
	mock.ExpectExec(regexp.QuoteMeta("UPDATE user_profiles SET preference_json = ?, budget_min = ?, budget_max = ?, profile_version = profile_version + 1 WHERE user_id = ?")).WithArgs([]byte(`{"tags":["gaming"]}`), "100.00", "200.00", uint64(7)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT preference_json, budget_min, budget_max, profile_version FROM user_profiles WHERE user_id = ?")).WithArgs(uint64(7)).WillReturnRows(sqlmock.NewRows([]string{"preference_json", "budget_min", "budget_max", "profile_version"}).AddRow([]byte(`{"tags":["gaming"]}`), "100.00", "200.00", 2))
	minimum, maximum := "100.00", "200.00"
	profile, err := NewRepository(db).UpdateProfile(context.Background(), 7, ProfileUpdate{PreferenceJSON: []byte(`{"tags":["gaming"]}`), BudgetMin: &minimum, BudgetMax: &maximum})
	if err != nil || profile.Version != 2 || profile.BudgetMin == nil || *profile.BudgetMin != "100.00" {
		t.Fatalf("UpdateProfile() = %#v, %v", profile, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRepositoryGetAddressScopesLookupToOwner(t *testing.T) {
	db, mock := newRepositoryMock(t)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id, receiver_name, receiver_phone, province, city, district, detail, is_default FROM user_addresses WHERE id = ? AND user_id = ?")).
		WithArgs(uint64(9), uint64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "receiver_name", "receiver_phone", "province", "city", "district", "detail", "is_default"}).AddRow(uint64(9), "Ada", "13800000000", "Beijing", "Beijing", "Haidian", "Road 1", true))
	got, err := NewRepository(db).GetAddress(context.Background(), 7, 9)
	if err != nil || got.ID != 9 || !got.IsDefault {
		t.Fatalf("GetAddress() = %#v, %v", got, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func newRepositoryMock(t *testing.T) (*sql.DB, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db, mock
}
