package httpserver

import (
	"net/http"

	"github.com/barakov-dot/tgproxy-panel/internal/auth"
)

type loginPageData struct {
	pageData
	Error string
}

func (s *Server) handleLoginForm(w http.ResponseWriter, r *http.Request) {
	// Already logged in — skip straight to the user list.
	if c, err := r.Cookie(sessionCookieName); err == nil && s.sessions.Verify(c.Value) {
		http.Redirect(w, r, s.base()+"/", http.StatusSeeOther)
		return
	}
	s.render(w, r, "login.html", loginPageData{pageData: s.newPageData("login")})
}

func (s *Server) handleLoginSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	key := r.RemoteAddr
	if !s.limiter.Allow(key) {
		s.render(w, r, "login.html", loginPageData{
			pageData: s.newPageData("login"),
			Error:    "Слишком много неудачных попыток входа. Попробуйте позже.",
		})
		return
	}

	login := r.FormValue("login")
	password := r.FormValue("password")

	if !auth.CheckLogin(s.cfg.AdminLogin, s.cfg.AdminPasswordHash, login, password) {
		s.limiter.RecordFailure(key)
		s.render(w, r, "login.html", loginPageData{
			pageData: s.newPageData("login"),
			Error:    "Неверный логин или пароль.",
		})
		return
	}
	s.limiter.RecordSuccess(key)

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    s.sessions.New(),
		Path:     "/",
		HttpOnly: true,
		// Secure is safe to hardcode: per plan.md §2 this process is only
		// ever reached through Caddy terminating TLS in front of it, so the
		// browser always sees an https:// origin even though this backend
		// itself speaks plain HTTP to Caddy on loopback. Testing directly
		// against http://127.0.0.1:$PANEL_PORT without Caddy in front will
		// not persist the cookie in a real browser — that's intentional.
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(auth.SessionLifetime.Seconds()),
	})
	http.Redirect(w, r, s.base()+"/", http.StatusSeeOther)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
	http.Redirect(w, r, s.base()+"/login", http.StatusSeeOther)
}
