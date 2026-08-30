// Package logging configures the process-wide slog.Logger.
package logging

import (
	"log/slog"
	"os"
)

// New returns a slog.Logger for the given format: "text" produces
// human-readable output with source locations for local development;
// anything else (including "") produces JSON, the production default for
// systemd/journald.
func New(format string) *slog.Logger {
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
