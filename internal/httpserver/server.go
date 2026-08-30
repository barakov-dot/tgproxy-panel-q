// Package httpserver implements tgproxy-panel's admin web panel: a chi
// router and html/template pages rendered with htmx-driven partial swaps.
// State-changing operations (issue/revoke/approve/deny) are delegated to
// internal/service, shared with internal/bot so the two never duplicate or
// diverge on that logic (plan.md §6).
package httpserver

import (
	"html/template"
	"log/slog"
	"net/http"

	"github.com/barakov-dot/tgproxy-panel/internal/auth"
	"github.com/barakov-dot/tgproxy-panel/internal/config"
	"github.com/barakov-dot/tgproxy-panel/internal/service"
)

// Server holds everything the panel's handlers need.
type Server struct {
	cfg     *config.Config
	store   userStore
	applier profileApplier
	actions *service.Actions

	sessions *auth.Sessions
	limiter  *auth.LoginLimiter

	tmpl *template.Template
	log  *slog.Logger
}

// New builds a Server and its chi router. store and ap are typically
// *store.Store and *applier.Applier; they're accepted as the narrower
// interfaces in interfaces.go so tests can pass fakes instead.
func New(cfg *config.Config, store userStore, ap profileApplier, sessions *auth.Sessions, limiter *auth.LoginLimiter, log *slog.Logger) (*Server, error) {
	tmpl, err := parseTemplates()
	if err != nil {
		return nil, err
	}
	if log == nil {
		log = slog.Default()
	}
	s := &Server{
		cfg:      cfg,
		store:    store,
		applier:  ap,
		actions:  service.New(store, ap, cfg.AutoIssue),
		sessions: sessions,
		limiter:  limiter,
		tmpl:     tmpl,
		log:      log,
	}
	return s, nil
}

// Handler returns the full chi router, ready to be handed to an
// *http.Server. See routes.go for the token-prefix mounting rationale.
func (s *Server) Handler() http.Handler {
	return s.routes()
}
