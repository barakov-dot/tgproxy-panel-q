package applier

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestWaitForReady_ImmediateSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/readyz" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	err := waitForReady(context.Background(), srv.Client(), srv.URL, 3, time.Millisecond)
	if err != nil {
		t.Fatalf("waitForReady() error = %v", err)
	}
}

func TestWaitForReady_SucceedsAfterRetries(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := attempts.Add(1)
		if n < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	err := waitForReady(context.Background(), srv.Client(), srv.URL, 5, time.Millisecond)
	if err != nil {
		t.Fatalf("waitForReady() error = %v", err)
	}
	if got := attempts.Load(); got != 3 {
		t.Errorf("attempts = %d, want 3", got)
	}
}

func TestWaitForReady_NeverReadyReturnsErrNotReady(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	err := waitForReady(context.Background(), srv.Client(), srv.URL, 3, time.Millisecond)
	if err == nil {
		t.Fatal("waitForReady() expected error, got nil")
	}
	if !strings.Contains(err.Error(), ErrNotReady.Error()) {
		t.Errorf("error %q does not wrap ErrNotReady", err.Error())
	}
}

func TestWaitForReady_ConnectionRefused(t *testing.T) {
	// Nothing listening on this URL; every attempt fails at the transport
	// level rather than with a non-200 status.
	err := waitForReady(context.Background(), http.DefaultClient, "http://127.0.0.1:1", 2, time.Millisecond)
	if err == nil {
		t.Fatal("waitForReady() expected error, got nil")
	}
	if !strings.Contains(err.Error(), ErrNotReady.Error()) {
		t.Errorf("error %q does not wrap ErrNotReady", err.Error())
	}
}

func TestWaitForReady_ContextCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := waitForReady(ctx, srv.Client(), srv.URL, 5, 50*time.Millisecond)
	if err == nil {
		t.Fatal("waitForReady() expected error, got nil")
	}
}
