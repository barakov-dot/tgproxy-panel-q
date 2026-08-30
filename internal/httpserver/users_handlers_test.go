package httpserver

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/barakov-dot/tgproxy-panel-q/internal/auth"
	"github.com/barakov-dot/tgproxy-panel-q/internal/models"
)

func (ts *testServer) authedRequest(t *testing.T, method, target string, body *strings.Reader) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if body != nil {
		req = httptest.NewRequest(method, target, body)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		req = httptest.NewRequest(method, target, nil)
	}
	req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: ts.loggedInCookie()})
	rr := httptest.NewRecorder()
	ts.Handler().ServeHTTP(rr, req)
	return rr
}

func TestUserListRendersUsers(t *testing.T) {
	tests := []struct {
		name string
	}{
		{name: "renders both users"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := newTestServer(t)
			ts.store.addUser(&models.User{TelegramID: 111, Username: strPtr("alice"), Status: models.StatusActive})
			ts.store.addUser(&models.User{TelegramID: 222, Status: models.StatusPending})

			rr := ts.authedRequest(t, http.MethodGet, ts.base()+"/", nil)
			if rr.Code != http.StatusOK {
				t.Fatalf("status = %d, body=%s", rr.Code, rr.Body.String())
			}
			body := rr.Body.String()
			if !strings.Contains(body, "111") || !strings.Contains(body, "222") {
				t.Errorf("expected both telegram IDs in list body:\n%s", body)
			}
		})
	}
}

func TestUserTablePartialSearchFilters(t *testing.T) {
	tests := []struct {
		name string
	}{
		{name: "filters by query"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := newTestServer(t)
			ts.store.addUser(&models.User{TelegramID: 111, Username: strPtr("alice"), Status: models.StatusActive})
			ts.store.addUser(&models.User{TelegramID: 222, Username: strPtr("bob"), Status: models.StatusPending})

			rr := ts.authedRequest(t, http.MethodGet, ts.base()+"/users?q=alice", nil)
			if rr.Code != http.StatusOK {
				t.Fatalf("status = %d", rr.Code)
			}
			body := rr.Body.String()
			if !strings.Contains(body, "111") {
				t.Errorf("expected telegram_id 111 (alice) in filtered body:\n%s", body)
			}
			if strings.Contains(body, "222") {
				t.Errorf("did not expect telegram_id 222 (bob) in filtered body:\n%s", body)
			}
		})
	}
}

func TestUserTablePendingFilter(t *testing.T) {
	tests := []struct {
		name string
	}{
		{name: "pending tab only"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := newTestServer(t)
			ts.store.addUser(&models.User{TelegramID: 111, Status: models.StatusActive})
			ts.store.addUser(&models.User{TelegramID: 222, Status: models.StatusPending})

			rr := ts.authedRequest(t, http.MethodGet, ts.base()+"/users?filter=pending", nil)
			body := rr.Body.String()
			if strings.Contains(body, "111") {
				t.Error("active user should not appear in the pending filter")
			}
			if !strings.Contains(body, "222") {
				t.Error("pending user should appear in the pending filter")
			}
		})
	}
}

func TestUserDetailRendersSecretAndLink(t *testing.T) {
	tests := []struct {
		name string
	}{
		{name: "active user detail"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := newTestServer(t)
			u := ts.store.addUser(&models.User{TelegramID: 111, Status: models.StatusActive, Secret: strPtr("deadbeef")})

			rr := ts.authedRequest(t, http.MethodGet, ts.base()+"/users/"+strconv.FormatInt(u.ID, 10), nil)
			if rr.Code != http.StatusOK {
				t.Fatalf("status = %d, body=%s", rr.Code, rr.Body.String())
			}
			body := rr.Body.String()
			if !strings.Contains(body, "deadbeef") {
				t.Errorf("expected secret in detail body:\n%s", body)
			}
			if !strings.Contains(body, "https://t.me/webproxy?server=proxy.example.com&amp;secret=deadbeef") {
				t.Errorf("expected profile link in detail body:\n%s", body)
			}
		})
	}
}

func TestUserDetailNotFound(t *testing.T) {
	ts := newTestServer(t)
	rr := ts.authedRequest(t, http.MethodGet, ts.base()+"/users/999", nil)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rr.Code)
	}
}

func TestApproveHandlerIssuesAndRerendersPartial(t *testing.T) {
	tests := []struct {
		name string
	}{
		{name: "approve pending"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := newTestServer(t)
			u := ts.store.addUser(&models.User{TelegramID: 111, Status: models.StatusPending})

			rr := ts.authedRequest(t, http.MethodPost, ts.base()+"/users/"+strconv.FormatInt(u.ID, 10)+"/approve", nil)
			if rr.Code != http.StatusOK {
				t.Fatalf("status = %d, body=%s", rr.Code, rr.Body.String())
			}
			if !strings.Contains(rr.Body.String(), "Активен") {
				t.Errorf("expected active status label in response:\n%s", rr.Body.String())
			}
			if ts.applier.IssueCalls != 1 {
				t.Errorf("IssueCalls = %d, want 1", ts.applier.IssueCalls)
			}
		})
	}
}

func TestRevokeHandlerAppliesAndShowsError(t *testing.T) {
	tests := []struct {
		name string
	}{
		{name: "revoke apply failure"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := newTestServer(t)
			ts.applier.RevokeErr = errForced
			u := ts.store.addUser(&models.User{TelegramID: 111, Status: models.StatusActive, Secret: strPtr("s"), ProfileName: strPtr("user_111")})

			rr := ts.authedRequest(t, http.MethodPost, ts.base()+"/users/"+strconv.FormatInt(u.ID, 10)+"/revoke", nil)
			if rr.Code != http.StatusOK {
				t.Fatalf("status = %d, body=%s", rr.Code, rr.Body.String())
			}
			if !strings.Contains(rr.Body.String(), "не удалось") {
				t.Errorf("expected a Russian failure message in response:\n%s", rr.Body.String())
			}
		})
	}
}

func TestDenyHandler(t *testing.T) {
	tests := []struct {
		name string
	}{
		{name: "deny pending"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := newTestServer(t)
			u := ts.store.addUser(&models.User{TelegramID: 111, Status: models.StatusPending})

			rr := ts.authedRequest(t, http.MethodPost, ts.base()+"/users/"+strconv.FormatInt(u.ID, 10)+"/deny", nil)
			if rr.Code != http.StatusOK {
				t.Fatalf("status = %d", rr.Code)
			}
			if !strings.Contains(rr.Body.String(), "Отклонён") {
				t.Errorf("expected denied status label:\n%s", rr.Body.String())
			}
		})
	}
}

func strPtr(s string) *string { return &s }
