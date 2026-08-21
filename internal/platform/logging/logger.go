// Package logging provides process-scoped logging primitives.
package logging

import (
	"io"
	"log/slog"
)

// New creates a JSON slog logger that writes to output at level.
func New(output io.Writer, level slog.Level) *slog.Logger {
	return slog.New(slog.NewJSONHandler(output, &slog.HandlerOptions{Level: level}))
}
