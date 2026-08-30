package httpserver

import (
	"net/http"
	"strconv"
	"testing"

	"github.com/barakov-dot/tgproxy-panel-q/internal/models"
)

func TestUserQRHandler(t *testing.T) {
	ts := newTestServer(t)
	u := ts.store.addUser(&models.User{TelegramID: 111, Status: models.StatusActive, Secret: strPtr("deadbeef")})

	rr := ts.authedRequest(t, http.MethodGet, ts.base()+"/users/"+strconv.FormatInt(u.ID, 10)+"/qr", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); ct != "image/png" {
		t.Errorf("Content-Type = %q, want image/png", ct)
	}
	if rr.Body.Len() == 0 {
		t.Error("expected non-empty PNG body")
	}
}

func TestUserQRHandlerNoSecret(t *testing.T) {
	ts := newTestServer(t)
	u := ts.store.addUser(&models.User{TelegramID: 111, Status: models.StatusPending})

	rr := ts.authedRequest(t, http.MethodGet, ts.base()+"/users/"+strconv.FormatInt(u.ID, 10)+"/qr", nil)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for a user with no secret", rr.Code)
	}
}
