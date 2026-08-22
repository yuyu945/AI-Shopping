package apperror_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/yuyu945/AI-Shopping/internal/platform/apperror"
)

func TestCodesAreStableAndSafe(t *testing.T) {
	codes := []apperror.Code{
		apperror.InvalidArgument,
		apperror.Unauthenticated,
		apperror.NotFound,
		apperror.OutOfStock,
		apperror.InsufficientBalance,
		apperror.PaymentInProgress,
		apperror.IdempotencyConflict,
		apperror.DependencyTimeout,
		apperror.Internal,
	}

	for _, code := range codes {
		t.Run(string(code), func(t *testing.T) {
			err := apperror.New(code, "safe message")
			if err.Code != code {
				t.Errorf("New(%q).Code = %q", code, err.Code)
			}
			if !strings.Contains(err.Error(), string(code)) {
				t.Errorf("Error() = %q, want code %q", err.Error(), code)
			}
		})
	}
}

func TestWrapDoesNotLeakCauseIntoSafeMessage(t *testing.T) {
	cause := errors.New("mysql password=highly-sensitive-value")
	err := apperror.Wrap(apperror.DependencyTimeout, "payment dependency timed out", cause)

	if err.Code != apperror.DependencyTimeout {
		t.Errorf("Wrap().Code = %q", err.Code)
	}
	if strings.Contains(err.Error(), "highly-sensitive-value") {
		t.Fatalf("Error() leaked cause: %q", err)
	}
	if !strings.Contains(err.Error(), "payment dependency timed out") {
		t.Errorf("Error() = %q, want safe message", err)
	}
	if !errors.Is(err, cause) {
		t.Error("errors.Is(Wrap(...), cause) = false")
	}
}
