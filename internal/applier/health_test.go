package applier

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestWaitForHealthy(t *testing.T) {
	tests := []struct {
		name        string
		serviceOut  string
		serviceErr  error
		readyStatus int
		attempts    int
		wantErr     bool
		wantSubstr  string
	}{
		{
			name:        "immediate success",
			serviceOut:  "active\n",
			readyStatus: http.StatusOK,
		},
		{
			name:        "service not active",
			serviceOut:  "inactive\n",
			readyStatus: http.StatusOK,
			attempts:    2,
			wantErr:     true,
			wantSubstr:  ErrNotReady.Error(),
		},
		{
			name:        "readyz not ok",
			serviceOut:  "active\n",
			readyStatus: http.StatusServiceUnavailable,
			attempts:    2,
			wantErr:     true,
			wantSubstr:  ErrNotReady.Error(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.readyStatus)
			}))
			defer srv.Close()

			runner := &fakeRunner{serviceOut: tt.serviceOut, serviceErr: tt.serviceErr}
			cfg := testApplierConfig(srv.URL)
			attempts := tt.attempts
			if attempts == 0 {
				attempts = 3
			}

			err := waitForHealthy(context.Background(), runner, srv.Client(), cfg, attempts, time.Millisecond)
			if tt.wantErr {
				if err == nil {
					t.Fatal("waitForHealthy() expected error")
				}
				if tt.wantSubstr != "" && !strings.Contains(err.Error(), tt.wantSubstr) {
					t.Errorf("error = %q, want substring %q", err.Error(), tt.wantSubstr)
				}
				return
			}
			if err != nil {
				t.Fatalf("waitForHealthy() error = %v", err)
			}
		})
	}
}

func TestCheckServiceActive(t *testing.T) {
	runner := &fakeRunner{serviceOut: "active\n"}
	if err := checkServiceActive(context.Background(), runner, "tproxy-server"); err != nil {
		t.Fatalf("checkServiceActive() error = %v", err)
	}
	if len(runner.serviceCalls) != 1 {
		t.Errorf("serviceCalls = %d, want 1", len(runner.serviceCalls))
	}
}
