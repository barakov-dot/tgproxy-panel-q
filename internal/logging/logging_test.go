package logging

import (
	"log/slog"
	"testing"
)

func TestNew(t *testing.T) {
	cases := []struct {
		format string
	}{
		{"json"},
		{"text"},
		{""},
	}
	for _, tc := range cases {
		t.Run(tc.format, func(t *testing.T) {
			logger := New(tc.format)
			if logger == nil {
				t.Fatalf("New(%q) returned nil", tc.format)
			}
			logger.Info("test message", "format", tc.format)
		})
	}
}

func TestSetDefault(t *testing.T) {
	logger := New("json")
	SetDefault(logger)
	if slog.Default() != logger {
		t.Error("SetDefault did not install logger as slog default")
	}
}
