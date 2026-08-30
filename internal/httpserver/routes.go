package httpserver

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/barakov-dot/tgproxy-panel-q/internal/auth"
)

// routes builds the chi router mounted under /{PanelPathToken}/.
func (s *Server) routes() http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)

	loginURL := s.base() + "/login"

	r.Route("/"+s.cfg.PanelPathToken, func(r chi.Router) {
		r.Handle("/static/*", http.StripPrefix("/"+s.cfg.PanelPathToken+"/static/", s.staticHandler()))

		r.Get("/login", s.handleLoginForm)
		r.Post("/login", s.handleLoginSubmit)
		r.Post("/logout", s.handleLogout)

		r.Group(func(r chi.Router) {
			r.Use(auth.RequireSession(s.sessions, loginURL))

			r.Get("/", s.handleUserList)
			r.Get("/users", s.handleUserTable)
			r.Get("/users/{id}", s.handleUserDetail)
			r.Get("/users/{id}/qr", s.handleUserQR)
			r.Post("/users/{id}/approve", s.handleApprove)
			r.Post("/users/{id}/deny", s.handleDeny)
			r.Post("/users/{id}/revoke", s.handleRevoke)
			r.Post("/users/{id}/send", s.handleSend)

			r.Get("/settings", s.handleSettings)
			r.Post("/settings/auto-issue", s.handleSetAutoIssue)
		})
	})

	return r
}

func (s *Server) base() string {
	return "/" + s.cfg.PanelPathToken
}
