package user

import (
	"context"
	"errors"
	"testing"
	"time"

	platformauth "github.com/yuyu945/AI-Shopping/internal/platform/auth"
)

func TestUserServiceRegisterNormalizesEmailAndIssuesToken(t *testing.T) {
	repo := &fakeRepository{create: func(_ context.Context, email, hash string) (User, error) {
		if email != "user@example.com" || hash != "hash" {
			t.Fatalf("email=%q hash=%q", email, hash)
		}
		return User{ID: 7, Email: email}, nil
	}}
	svc := NewUserService(repo, fakeHasher{}, newTestManager(t), func() time.Time { return time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC) })
	result, err := svc.Register(context.Background(), RegisterInput{Email: " User@Example.COM ", Password: "password-123"})
	if err != nil || result.User.ID != 7 || result.AccessToken == "" {
		t.Fatalf("Register() = %#v, %v", result, err)
	}
}

func TestUserServiceRejectsInvalidInputs(t *testing.T) {
	svc := NewUserService(&fakeRepository{}, fakeHasher{}, newTestManager(t), time.Now)
	for _, input := range []RegisterInput{{Email: "bad", Password: "password-123"}, {Email: "u@example.com", Password: "short"}} {
		if _, err := svc.Register(context.Background(), input); err == nil {
			t.Fatalf("Register(%#v) error=nil", input)
		}
	}
}

func TestUserServiceGetMyAddressReturnsOnlyOwnedAddress(t *testing.T) {
	address := Address{ID: 12, AddressInput: AddressInput{ReceiverName: "Ada", ReceiverPhone: "13800138000", Province: "Zhejiang", City: "Hangzhou", District: "Xihu", Detail: "No. 1"}}
	repo := &fakeRepository{address: func(_ context.Context, userID, addressID uint64) (Address, error) {
		if userID != 7 || addressID != 12 {
			return Address{}, &NotFoundError{Resource: "address"}
		}
		return address, nil
	}}
	svc := NewUserService(repo, fakeHasher{}, newTestManager(t), time.Now)
	got, err := svc.GetMyAddress(context.Background(), 7, 12)
	if err != nil || got != address {
		t.Fatalf("GetMyAddress() = %#v, %v", got, err)
	}
	_, err = svc.GetMyAddress(context.Background(), 8, 12)
	if err == nil {
		t.Fatal("GetMyAddress() foreign user error = nil")
	}
}

type fakeRepository struct {
	create  func(context.Context, string, string) (User, error)
	find    func(context.Context, string) (User, error)
	address func(context.Context, uint64, uint64) (Address, error)
}

func (f *fakeRepository) CreateUserWithProfile(ctx context.Context, email, hash string) (User, error) {
	if f.create == nil {
		return User{}, errors.New("not configured")
	}
	return f.create(ctx, email, hash)
}
func (f *fakeRepository) FindUserByEmail(ctx context.Context, email string) (User, error) {
	if f.find == nil {
		return User{}, &NotFoundError{Resource: "user"}
	}
	return f.find(ctx, email)
}

type fakeHasher struct{}

func (*fakeRepository) GetProfile(context.Context, uint64) (Profile, error) { return Profile{}, nil }
func (*fakeRepository) UpdateProfile(context.Context, uint64, ProfileUpdate) (Profile, error) {
	return Profile{}, nil
}
func (*fakeRepository) ListAddresses(context.Context, uint64) ([]Address, error) { return nil, nil }
func (f *fakeRepository) GetAddress(ctx context.Context, userID, addressID uint64) (Address, error) {
	if f.address == nil {
		return Address{}, &NotFoundError{Resource: "address"}
	}
	return f.address(ctx, userID, addressID)
}
func (*fakeRepository) CreateAddress(context.Context, uint64, AddressInput) (Address, error) {
	return Address{}, nil
}
func (*fakeRepository) UpdateAddress(context.Context, uint64, uint64, AddressInput) (Address, error) {
	return Address{}, nil
}
func (*fakeRepository) DeleteAddress(context.Context, uint64, uint64) error { return nil }

func (fakeHasher) Hash(string) (string, error)  { return "hash", nil }
func (fakeHasher) Compare(string, string) error { return nil }
func newTestManager(t *testing.T) *platformauth.Manager {
	t.Helper()
	manager, err := platformauth.NewManager([]byte("01234567890123456789012345678901"))
	if err != nil {
		t.Fatal(err)
	}
	return manager
}
