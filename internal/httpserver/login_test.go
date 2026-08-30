package httpserver

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/barakov-dot/tgproxy-panel-q/internal/auth"
)

func TestLoginSuccessSetsCookieAndRedirects(t *testing.T) {
	tests := []struct {
		name string
	}{
		{name: "success"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := newTestServer(t)
			h := ts.Handler()

			form := url.Values{"login": {"admin"}, "password": {testAdminPassword}}
			req := httptest.NewRequest(http.MethodPost, ts.base()+"/login", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)

			if rr.Code != http.StatusSeeOther {
				t.Fatalf("status = %d, want 303; body=%s", rr.Code, rr.Body.String())
			}
			cookies := rr.Result().Cookies()
			if len(cookies) != 1 || cookies[0].Name != auth.SessionCookieName || cookies[0].Value == "" {
				t.Fatalf("expected a session cookie to be set, got %+v", cookies)
			}
			if loc := rr.Header().Get("Location"); loc != ts.base()+"/" {
				t.Errorf("redirect location = %q, want %q", loc, ts.base()+"/")
			}
		})
	}
}

func TestLoginWrongPasswordShowsError(t *testing.T) {
	tests := []struct {
		name     string
		login    string
		password string
	}{
		{name: "wrong password", login: "admin", password: "wrong"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := newTestServer(t)
			form := url.Values{"login": {tt.login}, "password": {tt.password}}
			req := httptest.NewRequest(http.MethodPost, ts.base()+"/login", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			rr := httptest.NewRecorder()
			ts.Handler().ServeHTTP(rr, req)

			if rr.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rr.Code)
			}
			if !strings.Contains(rr.Body.String(), "Неверный логин или пароль") {
				t.Errorf("expected Russian login error message, body=%s", rr.Body.String())
			}
		})
	}
}

func TestLoginRequiresAuthForUsersList(t *testing.T) {
	ts := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, ts.base()+"/", nil)
	rr := httptest.NewRecorder()
	ts.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303 redirect to login", rr.Code)
	}
	if !strings.HasSuffix(rr.Header().Get("Location"), "/login") {
		t.Errorf("location = %q, want login redirect", rr.Header().Get("Location"))
	}
}

func TestLogoutClearsCookie(t *testing.T) {
	ts := newTestServer(t)
	h := ts.Handler()

	req := httptest.NewRequest(http.MethodPost, ts.base()+"/logout", nil)
	req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: ts.loggedInCookie()})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	cookies := rr.Result().Cookies()
	if len(cookies) != 1 || cookies[0].MaxAge >= 0 {
		t.Fatalf("expected an expiring cookie, got %+v", cookies)
	}
}

func TestLoginFormRedirectsWhenAlreadyAuthenticated(t *testing.T) {
	ts := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, ts.base()+"/login", nil)
	req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: ts.loggedInCookie()})
	rr := httptest.NewRecorder()
	ts.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rr.Code)
	}
	if rr.Header().Get("Location") != ts.base()+"/" {
		t.Errorf("location = %q", rr.Header().Get("Location"))
	}
}
