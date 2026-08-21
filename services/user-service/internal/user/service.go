package user

import (
	"context"
	"errors"
	"net/mail"
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
