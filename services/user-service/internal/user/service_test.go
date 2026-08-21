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

type fakeRepository struct {
	create func(context.Context, string, string) (User, error)
	find   func(context.Context, string) (User, error)
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
