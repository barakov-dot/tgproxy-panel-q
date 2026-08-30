package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequireSession(t *testing.T) {
	sessions := NewSessions("test-session-secret")
	loginURL := "/secretpath/login"
	okHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	mw := RequireSession(sessions, loginURL)
	handler := mw(okHandler)

	cases := []struct {
		name       string
		cookie     *http.Cookie
		wantStatus int
		wantLoc    string
	}{
		{
			name:       "no cookie redirects to login",
			wantStatus: http.StatusSeeOther,
			wantLoc:    loginURL,
		},
		{
			name:       "invalid cookie redirects to login",
			cookie:     &http.Cookie{Name: SessionCookieName, Value: "garbage"},
			wantStatus: http.StatusSeeOther,
			wantLoc:    loginURL,
		},
		{
			name:       "valid session passes through",
			cookie:     SessionCookie(sessions.New()),
			wantStatus: http.StatusOK,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/secretpath/", nil)
			if tc.cookie != nil {
				req.AddCookie(tc.cookie)
			}
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if rr.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", rr.Code, tc.wantStatus)
			}
			if tc.wantLoc != "" {
				if loc := rr.Header().Get("Location"); loc != tc.wantLoc {
					t.Errorf("Location = %q, want %q", loc, tc.wantLoc)
				}
			}
		})
	}
}

func TestSessionCookieAttributes(t *testing.T) {
	c := SessionCookie("signed-value")

	cases := []struct {
		name string
		got  any
		want any
	}{
		{"Name", c.Name, SessionCookieName},
		{"Value", c.Value, "signed-value"},
		{"Path", c.Path, "/"},
		{"HttpOnly", c.HttpOnly, true},
		{"Secure", c.Secure, true},
		{"SameSite", c.SameSite, http.SameSiteLaxMode},
		{"MaxAge", c.MaxAge, int(SessionLifetime.Seconds())},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("got %v, want %v", tc.got, tc.want)
			}
		})
	}
}

func TestClearSessionCookie(t *testing.T) {
	c := ClearSessionCookie()
	if c.MaxAge >= 0 {
		t.Errorf("MaxAge = %d, want negative (expired)", c.MaxAge)
	}
	if c.Name != SessionCookieName {
		t.Errorf("Name = %q, want %q", c.Name, SessionCookieName)
	}
}
