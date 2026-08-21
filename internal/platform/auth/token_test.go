package auth

import (
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v4"
)

const testSecret = "01234567890123456789012345678901"

func TestNewManagerRejectsShortSecret(t *testing.T) {
	if _, err := NewManager([]byte("short")); err == nil {
		t.Fatal("NewManager() error = nil, want short secret rejection")
	}
}

func TestManagerIssuesAndVerifiesBearer(t *testing.T) {
	now := time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC)
	manager, err := NewManager([]byte(testSecret))
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	token, expiresAt, err := manager.Issue(Principal{UserID: 42, Email: "user@example.com"}, now)
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	if want := now.Add(accessTokenTTL); !expiresAt.Equal(want) {
		t.Fatalf("expiresAt = %s, want %s", expiresAt, want)
	}
	principal, err := manager.VerifyBearer("Bearer "+token, now.Add(time.Hour))
	if err != nil {
		t.Fatalf("VerifyBearer() error = %v", err)
	}
	if principal != (Principal{UserID: 42, Email: "user@example.com"}) {
		t.Fatalf("principal = %#v", principal)
	}
}

func TestManagerRejectsInvalidBearerValuesWithoutLeakingThem(t *testing.T) {
	now := time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC)
	manager, err := NewManager([]byte(testSecret))
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	expired := signedToken(t, testSecret, jwt.MapClaims{
		"iss":   issuer,
		"sub":   "42",
		"email": "user@example.com",
		"iat":   now.Add(-2 * time.Hour).Unix(),
		"exp":   now.Add(-time.Hour).Unix(),
	})
	wrongIssuer := signedToken(t, testSecret, jwt.MapClaims{
		"iss":   "other-service",
		"sub":   "42",
		"email": "user@example.com",
		"iat":   now.Unix(),
		"exp":   now.Add(time.Hour).Unix(),
	})

	for name, bearer := range map[string]string{
		"missing prefix": "not-a-bearer",
		"expired":        "Bearer " + expired,
		"wrong issuer":   "Bearer " + wrongIssuer,
	} {
		t.Run(name, func(t *testing.T) {
			_, verifyErr := manager.VerifyBearer(bearer, now)
			if verifyErr == nil {
				t.Fatal("VerifyBearer() error = nil")
			}
			if strings.Contains(verifyErr.Error(), testSecret) || strings.Contains(verifyErr.Error(), bearer) {
				t.Fatalf("VerifyBearer() leaked sensitive value: %v", verifyErr)
			}
		})
	}
}

func signedToken(t *testing.T, secret string, claims jwt.Claims) string {
	t.Helper()
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("SignedString() error = %v", err)
	}
	return token
}
