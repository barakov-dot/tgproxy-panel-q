package auth

import (
	"net/http"
)

// SessionCookie builds an HttpOnly session cookie with Secure and
// SameSite=Lax. Path is "/" so the cookie is sent for all panel routes
// under the secret path prefix.
func SessionCookie(value string) *http.Cookie {
	return &http.Cookie{
		Name:     SessionCookieName,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		// Secure is safe to hardcode: per plan.md §2 this process is only
		// ever reached through Caddy terminating TLS in front of it, so the
		// browser always sees an https:// origin even though this backend
		// itself speaks plain HTTP to Caddy on loopback.
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(SessionLifetime.Seconds()),
	}
}

// ClearSessionCookie returns a cookie that clears the session in the browser.
func ClearSessionCookie() *http.Cookie {
	c := SessionCookie("")
	c.MaxAge = -1
	return c
}

// RequireSession returns middleware that redirects unauthenticated requests
// to loginURL (typically base+"/login"). loginURL must be an absolute path
// such as "/<PANEL_PATH_TOKEN>/login".
func RequireSession(sessions *Sessions, loginURL string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c, err := r.Cookie(SessionCookieName)
			if err != nil || !sessions.Verify(c.Value) {
				http.Redirect(w, r, loginURL, http.StatusSeeOther)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
