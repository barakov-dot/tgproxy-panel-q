// Package logging configures the process-wide structured logger.
package logging

import (
	"log/slog"
	"os"
)

// Logger is the standard logger type used across tgproxy-panel packages.
type Logger = *slog.Logger

// New returns a Logger for the given format: "text" for human-readable output
// with source locations; anything else (including "") uses JSON (production default).
func New(format string) Logger {
	opts := &slog.HandlerOptions{
		AddSource: format == "text",
	}

	var handler slog.Handler
	if format == "text" {
		handler = slog.NewTextHandler(os.Stderr, opts)
	} else {
		handler = slog.NewJSONHandler(os.Stderr, opts)
	}
	return slog.New(handler)
}

// SetDefault installs logger as the process-wide default slog logger.
func SetDefault(logger Logger) {
	slog.SetDefault(logger)
}
