package httpserver

import (
	"log/slog"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/barakov-dot/tgproxy-panel/internal/auth"
	"github.com/barakov-dot/tgproxy-panel/internal/config"
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

	srv, err := New(cfg, fs, fa, sessions, limiter, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return &testServer{Server: srv, store: fs, applier: fa, cfg: cfg}
}

// loggedInCookie returns a valid session cookie value for use in requests.
func (ts *testServer) loggedInCookie() string {
	return ts.sessions.New()
}
