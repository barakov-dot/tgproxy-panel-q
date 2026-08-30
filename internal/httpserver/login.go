package httpserver

import (
	"net/http"

	"github.com/barakov-dot/tgproxy-panel-q/internal/auth"
)

type loginPageData struct {
	pageData
	Error string
}

func (s *Server) handleLoginForm(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(auth.SessionCookieName); err == nil && s.sessions.Verify(c.Value) {
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

	http.SetCookie(w, auth.SessionCookie(s.sessions.New()))
	http.Redirect(w, r, s.base()+"/", http.StatusSeeOther)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, auth.ClearSessionCookie())
	http.Redirect(w, r, s.base()+"/login", http.StatusSeeOther)
}
