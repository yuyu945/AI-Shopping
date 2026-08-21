// Package auth provides JWT authentication primitives shared by the Gateway
// and user-service.
package auth

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v4"
)

const (
	issuer         = "ai-shopping"
	accessTokenTTL = 24 * time.Hour
	minimumSecret  = 32
)

var errInvalidBearer = errors.New("invalid bearer token")

type contextKey uint8

const (
	principalKey contextKey = iota
	bearerKey
)

// Principal is the verified identity attached to an authenticated request.
type Principal struct {
	UserID uint64
	Email  string
}

// Manager signs and verifies the MVP access token format.
type Manager struct{ secret []byte }

// NewManager constructs a token manager from a non-empty, sufficiently long secret.
func NewManager(secret []byte) (*Manager, error) {
	if len(secret) < minimumSecret {
		return nil, fmt.Errorf("jwt secret must be at least %d bytes", minimumSecret)
	}
	return &Manager{secret: append([]byte(nil), secret...)}, nil
}

// Issue creates a 24-hour HS256 access token for principal.
func (m *Manager) Issue(principal Principal, now time.Time) (string, time.Time, error) {
	if m == nil || len(m.secret) < minimumSecret || principal.UserID == 0 || strings.TrimSpace(principal.Email) == "" {
		return "", time.Time{}, errInvalidBearer
	}
	expiresAt := now.Add(accessTokenTTL)
	claims := jwt.MapClaims{
		"iss":   issuer,
		"sub":   strconv.FormatUint(principal.UserID, 10),
		"email": principal.Email,
		"iat":   now.Unix(),
		"exp":   expiresAt.Unix(),
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.secret)
	if err != nil {
		return "", time.Time{}, errInvalidBearer
	}
	return token, expiresAt, nil
}

// VerifyBearer verifies a bearer token and returns its trusted principal.
func (m *Manager) VerifyBearer(bearer string, now time.Time) (Principal, error) {
	if m == nil || len(m.secret) < minimumSecret {
		return Principal{}, errInvalidBearer
	}
	parts := strings.Fields(bearer)
	if len(parts) != 2 || parts[0] != "Bearer" || parts[1] == "" {
		return Principal{}, errInvalidBearer
	}
	claims := jwt.MapClaims{}
	parser := jwt.Parser{SkipClaimsValidation: true}
	token, err := parser.ParseWithClaims(parts[1], claims, func(token *jwt.Token) (any, error) {
		if token.Method == nil || token.Method.Alg() != jwt.SigningMethodHS256.Alg() {
			return nil, errInvalidBearer
		}
		return m.secret, nil
	})
	if err != nil || token == nil || !token.Valid {
		return Principal{}, errInvalidBearer
	}
	if !claims.VerifyIssuer(issuer, true) || !claims.VerifyIssuedAt(now.Unix(), true) || !claims.VerifyExpiresAt(now.Unix(), true) {
		return Principal{}, errInvalidBearer
	}
	userID, err := strconv.ParseUint(claimString(claims, "sub"), 10, 64)
	if err != nil || userID == 0 {
		return Principal{}, errInvalidBearer
	}
	email := strings.TrimSpace(claimString(claims, "email"))
	if email == "" {
		return Principal{}, errInvalidBearer
	}
	return Principal{UserID: userID, Email: email}, nil
}

func claimString(claims jwt.MapClaims, name string) string {
	value, _ := claims[name].(string)
	return value
}

// ContextWithPrincipal returns ctx with principal attached.
func ContextWithPrincipal(ctx context.Context, principal Principal) context.Context {
	return context.WithValue(ctx, principalKey, principal)
}

// PrincipalFromContext returns the verified principal stored in ctx.
func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	if ctx == nil {
		return Principal{}, false
	}
	principal, ok := ctx.Value(principalKey).(Principal)
	return principal, ok && principal.UserID != 0 && principal.Email != ""
}

// ContextWithBearer returns ctx with an authenticated bearer value attached.
func ContextWithBearer(ctx context.Context, bearer string) context.Context {
	return context.WithValue(ctx, bearerKey, bearer)
}

// BearerFromContext returns the authenticated bearer value stored in ctx.
func BearerFromContext(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	bearer, ok := ctx.Value(bearerKey).(string)
	return bearer, ok && bearer != ""
}
