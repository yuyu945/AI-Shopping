package user

import (
	"context"
	"errors"
	"net/mail"
	"regexp"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/yuyu945/AI-Shopping/internal/platform/apperror"
	platformauth "github.com/yuyu945/AI-Shopping/internal/platform/auth"
)

type RegisterInput struct{ Email, Password string }
type AuthResult struct {
	AccessToken string
	ExpiresAt   time.Time
	User        User
}
type userRepository interface {
	CreateUserWithProfile(context.Context, string, string) (User, error)
	FindUserByEmail(context.Context, string) (User, error)
	GetProfile(context.Context, uint64) (Profile, error)
	UpdateProfile(context.Context, uint64, ProfileUpdate) (Profile, error)
	ListAddresses(context.Context, uint64) ([]Address, error)
	CreateAddress(context.Context, uint64, AddressInput) (Address, error)
	UpdateAddress(context.Context, uint64, uint64, AddressInput) (Address, error)
	DeleteAddress(context.Context, uint64, uint64) error
}

var decimalPattern = regexp.MustCompile(`^\d{1,10}(?:\.\d{1,2})?$`)

func (s *UserService) GetMyProfile(ctx context.Context, userID uint64) (Profile, error) {
	v, err := s.repo.GetProfile(ctx, userID)
	return v, safeResourceError(err)
}
func (s *UserService) UpdateMyProfile(ctx context.Context, userID uint64, update ProfileUpdate) (Profile, error) {
	if (update.BudgetMin != nil && !decimalPattern.MatchString(*update.BudgetMin)) || (update.BudgetMax != nil && !decimalPattern.MatchString(*update.BudgetMax)) || (update.BudgetMin != nil && update.BudgetMax != nil && *update.BudgetMin > *update.BudgetMax) {
		return Profile{}, apperror.New(apperror.InvalidArgument, "invalid budget range")
	}
	v, err := s.repo.UpdateProfile(ctx, userID, update)
	return v, safeResourceError(err)
}
func (s *UserService) ListMyAddresses(ctx context.Context, userID uint64) ([]Address, error) {
	v, err := s.repo.ListAddresses(ctx, userID)
	return v, safeResourceError(err)
}
func (s *UserService) CreateMyAddress(ctx context.Context, userID uint64, input AddressInput) (Address, error) {
	if err := validateAddress(input); err != nil {
		return Address{}, err
	}
	v, err := s.repo.CreateAddress(ctx, userID, input)
	return v, safeResourceError(err)
}
func (s *UserService) UpdateMyAddress(ctx context.Context, userID, addressID uint64, input AddressInput) (Address, error) {
	if addressID == 0 {
		return Address{}, apperror.New(apperror.InvalidArgument, "address id is required")
	}
	if err := validateAddress(input); err != nil {
		return Address{}, err
	}
	v, err := s.repo.UpdateAddress(ctx, userID, addressID, input)
	return v, safeResourceError(err)
}
func (s *UserService) DeleteMyAddress(ctx context.Context, userID, addressID uint64) error {
	if addressID == 0 {
		return apperror.New(apperror.InvalidArgument, "address id is required")
	}
	return safeResourceError(s.repo.DeleteAddress(ctx, userID, addressID))
}

func safeResourceError(err error) error {
	var notFound *NotFoundError
	if errors.As(err, &notFound) {
		return apperror.New(apperror.NotFound, "resource not found")
	}
	return err
}
func validateAddress(input AddressInput) error {
	if strings.TrimSpace(input.ReceiverName) == "" || strings.TrimSpace(input.ReceiverPhone) == "" || strings.TrimSpace(input.Province) == "" || strings.TrimSpace(input.City) == "" || strings.TrimSpace(input.District) == "" || strings.TrimSpace(input.Detail) == "" || len(input.ReceiverName) > 128 || len(input.ReceiverPhone) > 32 || len(input.Province) > 64 || len(input.City) > 64 || len(input.District) > 64 || len(input.Detail) > 512 {
		return apperror.New(apperror.InvalidArgument, "invalid address")
	}
	return nil
}

type passwordHasher interface {
	Hash(string) (string, error)
	Compare(string, string) error
}
type tokenIssuer interface {
	Issue(platformauth.Principal, time.Time) (string, time.Time, error)
}
type UserService struct {
	repo   userRepository
	hasher passwordHasher
	tokens tokenIssuer
	now    func() time.Time
}

func NewUserService(repo userRepository, hasher passwordHasher, tokens tokenIssuer, now func() time.Time) *UserService {
	if hasher == nil {
		hasher = bcryptHasher{}
	}
	if now == nil {
		now = time.Now
	}
	return &UserService{repo: repo, hasher: hasher, tokens: tokens, now: now}
}
func (s *UserService) Register(ctx context.Context, input RegisterInput) (AuthResult, error) {
	email, err := validCredentials(input)
	if err != nil {
		return AuthResult{}, err
	}
	hash, err := s.hasher.Hash(input.Password)
	if err != nil {
		return AuthResult{}, apperror.New(apperror.Internal, "registration failed")
	}
	user, err := s.repo.CreateUserWithProfile(ctx, email, hash)
	if err != nil {
		var duplicate DuplicateEmailError
		if errors.As(err, &duplicate) {
			return AuthResult{}, apperror.New(apperror.InvalidArgument, "registration could not be completed")
		}
		return AuthResult{}, apperror.New(apperror.Internal, "registration failed")
	}
	return s.issue(user)
}
func (s *UserService) Login(ctx context.Context, input RegisterInput) (AuthResult, error) {
	email, err := validCredentials(input)
	if err != nil {
		return AuthResult{}, apperror.New(apperror.Unauthenticated, "invalid email or password")
	}
	user, err := s.repo.FindUserByEmail(ctx, email)
	if err != nil {
		return AuthResult{}, apperror.New(apperror.Unauthenticated, "invalid email or password")
	}
	if s.hasher.Compare(user.PasswordHash, input.Password) != nil {
		return AuthResult{}, apperror.New(apperror.Unauthenticated, "invalid email or password")
	}
	return s.issue(user)
}
func (s *UserService) issue(user User) (AuthResult, error) {
	token, expires, err := s.tokens.Issue(platformauth.Principal{UserID: user.ID, Email: user.Email}, s.now())
	if err != nil {
		return AuthResult{}, apperror.New(apperror.Internal, "authentication failed")
	}
	return AuthResult{AccessToken: token, ExpiresAt: expires, User: user}, nil
}
func validCredentials(input RegisterInput) (string, error) {
	email := strings.ToLower(strings.TrimSpace(input.Email))
	parsed, err := mail.ParseAddress(email)
	if err != nil || parsed.Address != email || len([]byte(input.Password)) < 8 || len([]byte(input.Password)) > 72 {
		return "", apperror.New(apperror.InvalidArgument, "invalid credentials")
	}
	return email, nil
}

type bcryptHasher struct{}

func (bcryptHasher) Hash(value string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(value), bcrypt.DefaultCost)
	return string(hash), err
}
func (bcryptHasher) Compare(hash, value string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(value))
}
