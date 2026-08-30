package httpserver

import (
	"fmt"
	"log/slog"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/barakov-dot/tgproxy-panel-q/internal/auth"
	"github.com/barakov-dot/tgproxy-panel-q/internal/config"
	"github.com/barakov-dot/tgproxy-panel-q/internal/service"
)

const testAdminPassword = "correct horse battery staple"

func testConfig(t *testing.T, backupDir string) *config.Config {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(testAdminPassword), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("bcrypt: %v", err)
	}
	return &config.Config{
		PanelPathToken:    "testtoken1234567890",
		AdminLogin:        "admin",
		AdminPasswordHash: string(hash),
		SessionSecret:     "test-session-secret-at-least-16-bytes",
		TproxyHostname:    "proxy.example.com",
		BackupDir:         backupDir,
	}
}

type testServer struct {
	*Server
	store   *fakeStore
	applier *fakeApplier
	cfg     *config.Config
}

func newTestServer(t *testing.T) *testServer {
	t.Helper()
	fs := newFakeStore()
	fa := &fakeApplier{}
	cfg := testConfig(t, t.TempDir())

	sessions := auth.NewSessions(cfg.SessionSecret)
	limiter := auth.NewDefaultLoginLimiter()
	svc := service.New(cfg, fs, fa, nil)
	svc.GenSecret = func() (string, error) { return "deadbeefdeadbeefdeadbeefdeadbeef", nil }
	svc.ProfileName = func(id int64) string { return fmt.Sprintf("user_%d", id) }

	srv, err := New(cfg, svc, sessions, limiter, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return &testServer{Server: srv, store: fs, applier: fa, cfg: cfg}
}

func (ts *testServer) loggedInCookie() string {
	return ts.sessions.New()
}

func (ts *testServer) base() string {
	return "/" + ts.cfg.PanelPathToken
}
