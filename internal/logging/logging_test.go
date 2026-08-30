package logging

import "testing"

func TestNew(t *testing.T) {
	for _, format := range []string{"json", "text", ""} {
		logger := New(format)
		if logger == nil {
			t.Fatalf("New(%q) returned nil", format)
		}
		logger.Info("test message", "format", format)
	}
}
