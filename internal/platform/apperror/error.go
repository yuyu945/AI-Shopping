// Package apperror defines stable, API-safe application error codes.
package apperror

// Code is a stable application error code suitable for API responses.
type Code string

const (
	// InvalidArgument indicates invalid client input.
	InvalidArgument Code = "INVALID_ARGUMENT"
	// Unauthenticated indicates missing or invalid authentication.
	Unauthenticated Code = "UNAUTHENTICATED"
	// NotFound indicates a requested resource does not exist.
	NotFound Code = "NOT_FOUND"
	// OutOfStock indicates the requested inventory is unavailable.
	OutOfStock Code = "OUT_OF_STOCK"
	// IdempotencyConflict indicates a request ID conflicts with a prior request.
	IdempotencyConflict Code = "IDEMPOTENCY_CONFLICT"
	// DependencyTimeout indicates an external dependency exceeded its timeout.
	DependencyTimeout Code = "DEPENDENCY_TIMEOUT"
	// Internal indicates an unexpected internal failure.
	Internal Code = "INTERNAL"
)

// Error is an application error with a message safe for API clients.
type Error struct {
	Code    Code
	Message string
	cause   error
}

// New creates an application error with an API-safe message.
func New(code Code, message string) *Error {
	return &Error{Code: code, Message: message}
}

// Wrap creates an application error with an API-safe message while retaining
// the cause for internal inspection. The cause text is never included in Error.
func Wrap(code Code, message string, cause error) *Error {
	return &Error{Code: code, Message: message, cause: cause}
}

// Error returns only the stable code and API-safe message.
func (e *Error) Error() string {
	if e.Message == "" {
		return string(e.Code)
	}
	return string(e.Code) + ": " + e.Message
}

// Unwrap returns the internal cause for diagnostics and errors.Is checks.
func (e *Error) Unwrap() error {
	return e.cause
}
