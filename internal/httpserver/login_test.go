package httpserver

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/barakov-dot/tgproxy-panel/internal/auth"
	"github.com/barakov-dot/tgproxy-panel/internal/models"
)

func TestLoginSuccessSetsCookieAndRedirects(t *testing.T) {
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
	if len(cookies) != 1 || cookies[0].Name != sessionCookieName || cookies[0].Value == "" {
		t.Fatalf("expected a session cookie to be set, got %+v", cookies)
	}
	if loc := rr.Header().Get("Location"); loc != ts.base()+"/" {
		t.Errorf("redirect location = %q, want %q", loc, ts.base()+"/")
	}
}

func TestLoginWrongPasswordShowsError(t *testing.T) {
	ts := newTestServer(t)
	h := ts.Handler()

	form := url.Values{"login": {"admin"}, "password": {"wrong"}}
	req := httptest.NewRequest(http.MethodPost, ts.base()+"/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (re-rendered login form)", rr.Code)
	}
	if len(rr.Result().Cookies()) != 0 {
		t.Error("no cookie should be set on a failed login")
	}
	if !strings.Contains(rr.Body.String(), "Неверный логин") {
		t.Errorf("expected Russian error message in body, got: %s", rr.Body.String())
	}
}

func TestLoginLockoutAfterRepeatedFailures(t *testing.T) {
	ts := newTestServer(t)
	h := ts.Handler()

	attempt := func(password string) *httptest.ResponseRecorder {
		form := url.Values{"login": {"admin"}, "password": {password}}
		req := httptest.NewRequest(http.MethodPost, ts.base()+"/login", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.RemoteAddr = "203.0.113.1:12345"
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		return rr
	}

	for i := 0; i < auth.DefaultMaxAttempts; i++ {
		attempt("wrong")
	}

	// One more attempt, even with the correct password, should now be
	// rejected by the rate limiter rather than reach the bcrypt check.
	rr := attempt(testAdminPassword)
	if len(rr.Result().Cookies()) != 0 {
		t.Error("expected lockout to block even a correct password")
	}
	if !strings.Contains(rr.Body.String(), "Слишком много") {
		t.Errorf("expected lockout message, got: %s", rr.Body.String())
	}
}

func TestAuthMiddlewareRedirectsWithoutSession(t *testing.T) {
	ts := newTestServer(t)
	h := ts.Handler()

	req := httptest.NewRequest(http.MethodGet, ts.base()+"/", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303 redirect to login", rr.Code)
	}
	if loc := rr.Header().Get("Location"); loc != ts.base()+"/login" {
		t.Errorf("redirect location = %q, want %q", loc, ts.base()+"/login")
	}
}

func TestAuthMiddlewareAllowsValidSession(t *testing.T) {
	ts := newTestServer(t)
	ts.store.addUser(&models.User{TelegramID: 42, Status: models.StatusPending})
	h := ts.Handler()

	req := httptest.NewRequest(http.MethodGet, ts.base()+"/", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: ts.loggedInCookie()})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
}

func TestBareRootPath404s(t *testing.T) {
	ts := newTestServer(t)
	h := ts.Handler()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("bare '/' status = %d, want 404 (must not leak that the panel exists)", rr.Code)
	}
}

func TestLogoutClearsCookie(t *testing.T) {
	ts := newTestServer(t)
	h := ts.Handler()

	req := httptest.NewRequest(http.MethodPost, ts.base()+"/logout", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	cookies := rr.Result().Cookies()
	if len(cookies) != 1 || cookies[0].MaxAge >= 0 {
		t.Fatalf("expected an expiring cookie, got %+v", cookies)
	}
}
