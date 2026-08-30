package httpserver

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// sessionCookieName is the panel's session cookie.
const sessionCookieName = "tgproxy_panel_session"

// routes builds the chi router.
//
// Token-prefix mounting rationale (see plan.md §8's Caddyfile example): Caddy's
// `handle /<TOKEN>/* { reverse_proxy 127.0.0.1:9000 }` forwards the request
// path as-is — there is no `uri strip_prefix` directive in the reference
// Caddyfile, so the panel process itself receives requests with the token
// still in the path (e.g. "/<TOKEN>/users/17", not "/users/17"). The router
// is therefore mounted entirely under "/" + cfg.PanelPathToken; a request to
// the bare "/" (or any path without the token) matches nothing and falls
// through to chi's default 404 — a scanner hitting the bare domain/port
// learns nothing about the panel existing, consistent with plan.md §5's
// "секретный путь" design.
func (s *Server) routes() http.Handler {
	r := chi.NewRouter()

	// middleware.RealIP rewrites r.RemoteAddr from X-Forwarded-For/X-Real-IP.
	// That's normally risky (any client can forge those headers), but this
	// process only ever listens on 127.0.0.1 (Config.PanelPort) and is only
	// ever reached through Caddy's reverse_proxy in front of it (plan.md
	// §2) — so every request this process sees has already had its
	// X-Forwarded-For set by a proxy we trust, never by an arbitrary
	// internet client talking to us directly. It must run before the login
	// rate limiter below, which keys off r.RemoteAddr.
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)

	r.Route("/"+s.cfg.PanelPathToken, func(r chi.Router) {
		r.Get("/login", s.handleLoginForm)
		r.Post("/login", s.handleLoginSubmit)
		r.Post("/logout", s.handleLogout)

		r.Group(func(r chi.Router) {
			r.Use(s.requireAuth)

			r.Get("/", s.handleUserList)
			r.Get("/users/table", s.handleUserTable)
			r.Get("/users/{id}", s.handleUserDetail)
			r.Get("/users/{id}/qr.png", s.handleUserQR)
			r.Post("/users/{id}/approve", s.handleApprove)
			r.Post("/users/{id}/deny", s.handleDeny)
			r.Post("/users/{id}/revoke", s.handleRevoke)
			r.Post("/users/{id}/notify", s.handleNotify)

			r.Get("/settings", s.handleSettings)
			r.Post("/settings/auto-issue", s.handleSetAutoIssue)
		})
	})

	return r
}

// base returns the URL path prefix ("/" + PanelPathToken) every in-panel
// link must be built from, since the router is mounted under it (see
// routes' doc comment).
func (s *Server) base() string {
	return "/" + s.cfg.PanelPathToken
}

// requireAuth redirects to the login page (relative to the token prefix)
// unless the request carries a valid session cookie.
func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(sessionCookieName)
		if err != nil || !s.sessions.Verify(c.Value) {
			http.Redirect(w, r, s.base()+"/login", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}
