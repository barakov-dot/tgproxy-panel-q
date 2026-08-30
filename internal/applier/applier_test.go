package applier

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/barakov-dot/tgproxy-panel-q/internal/config"
	"github.com/barakov-dot/tgproxy-panel-q/internal/models"
	"github.com/barakov-dot/tgproxy-panel-q/internal/store"
)

type fakeRunner struct {
	checkErr    error
	checkStderr string
	checkCalls  [][]string

	applyErr    error
	applyStdout string
	applyStderr string
	applyCalls  [][]string

	serviceOut   string
	serviceErr   error
	serviceCalls [][]string
}

func (f *fakeRunner) Run(ctx context.Context, name string, args ...string) (string, string, error) {
	switch name {
	case "systemctl":
		f.serviceCalls = append(f.serviceCalls, args)
		return f.serviceOut, "", f.serviceErr
	case "sudo":
		f.applyCalls = append(f.applyCalls, args)
		return f.applyStdout, f.applyStderr, f.applyErr
	default:
		if strings.Contains(name, "tproxy-server") || strings.HasSuffix(name, "tproxy-server") {
			f.checkCalls = append(f.checkCalls, args)
			return "", f.checkStderr, f.checkErr
		}
		f.applyCalls = append(f.applyCalls, append([]string{name}, args...))
		return f.applyStdout, f.applyStderr, f.applyErr
	}
}

func testApplierConfig(readyzURL string) *config.Config {
	return &config.Config{
		TproxyBackend:       "127.0.0.1:2398",
		TproxyCarrierMode:   "https",
		TproxyConfigPath:    "/etc/tproxy-server/config.json",
		TproxyServerBin:     "",
		TproxyServiceName:   "tproxy-server",
		TproxyAdminURL:      readyzURL,
		ApplyProfilesScript: "/opt/tgproxy-panel/bin/apply-profiles.sh",
		BackupDir:           "",
		BackupKeep:          100,
	}
}

func newTestApplier(t *testing.T, s storeIface, readyzURL string, runner *fakeRunner, dir string) *Applier {
	t.Helper()
	cfg := testApplierConfig(readyzURL)
	cfg.BackupDir = dir
	cfg.TproxyProfilesPath = filepath.Join(dir, "profiles.json")
	a := &Applier{
		cfg:                 cfg,
		store:               s,
		runner:              runner,
		httpClient:          &http.Client{Timeout: time.Second},
		healthCheckAttempts: 3,
		healthCheckInterval: time.Millisecond,
	}
	runner.serviceOut = "active\n"
	return a
}

func readyServer(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestApplier_ApplyFlow(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	seedProfiles := filepath.Join(dir, "profiles.json")
	seed := &ProfilesFile{Profiles: []Profile{
		{Name: "seed", Secret: "000102030405060708090a0b0c0d0e0f", Backend: "127.0.0.1:2398", CarrierMode: "https"},
	}}
	if err := WriteProfiles(seedProfiles, seed); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name       string
		setup      func(t *testing.T, s *store.Store) (tgID int64, profileName, secret string)
		action     func(a *Applier, tgID int64, profileName, secret string) error
		runner     func() *fakeRunner
		wantErr    error
		wantCount  int
		wantApply  int
		wantCheck  int
		errSubstr  string
	}{
		{
			name: "issue success",
			setup: func(t *testing.T, s *store.Store) (int64, string, string) {
				issueUser(t, ctx, s, 555, "user_555", "cccccccccccccccccccccccccccccccc")
				return 555, "user_555", "cccccccccccccccccccccccccccccccc"
			},
			action: func(a *Applier, tgID int64, profileName, secret string) error {
				_, err := a.IssueProfile(ctx, tgID, profileName, secret)
				return err
			},
			runner:    func() *fakeRunner { return &fakeRunner{} },
			wantCount: 1,
			wantApply: 1,
		},
		{
			name: "revoke success",
			setup: func(t *testing.T, s *store.Store) (int64, string, string) {
				issueUser(t, ctx, s, 666, "user_666", "dddddddddddddddddddddddddddddddd")
				revokeUser(t, ctx, s, 666)
				return 666, "", ""
			},
			action: func(a *Applier, tgID int64, _, _ string) error {
				_, err := a.RevokeProfile(ctx, tgID)
				return err
			},
			runner:    func() *fakeRunner { return &fakeRunner{} },
			wantCount: 0,
			wantApply: 1,
		},
		{
			name: "state mismatch",
			setup: func(t *testing.T, s *store.Store) (int64, string, string) {
				issueUser(t, ctx, s, 555, "user_555", "cccccccccccccccccccccccccccccccc")
				return 555, "wrong", "cccccccccccccccccccccccccccccccc"
			},
			action: func(a *Applier, tgID int64, profileName, secret string) error {
				_, err := a.IssueProfile(ctx, tgID, profileName, secret)
				return err
			},
			runner:    func() *fakeRunner { return &fakeRunner{} },
			wantErr:   ErrStateMismatch,
			wantApply: 0,
		},
		{
			name: "validation failure",
			setup: func(t *testing.T, s *store.Store) (int64, string, string) {
				issueUser(t, ctx, s, 777, "user_777", "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee")
				return 777, "user_777", "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
			},
			action: func(a *Applier, tgID int64, profileName, secret string) error {
				_, err := a.IssueProfile(ctx, tgID, profileName, secret)
				return err
			},
			runner: func() *fakeRunner {
				return &fakeRunner{checkErr: errors.New("exit 1"), checkStderr: "invalid profile"}
			},
			wantErr:   ErrValidationFailed,
			wantApply: 0,
			wantCheck: 1,
			errSubstr: "invalid profile",
		},
		{
			name: "apply script failure",
			setup: func(t *testing.T, s *store.Store) (int64, string, string) {
				issueUser(t, ctx, s, 888, "user_888", "ffffffffffffffffffffffffffffffff")
				return 888, "user_888", "ffffffffffffffffffffffffffffffff"
			},
			action: func(a *Applier, tgID int64, profileName, secret string) error {
				_, err := a.IssueProfile(ctx, tgID, profileName, secret)
				return err
			},
			runner: func() *fakeRunner {
				return &fakeRunner{applyErr: errors.New("exit 1"), applyStderr: "restart failed"}
			},
			wantErr:   ErrApplyScriptFailed,
			wantApply: 1,
			errSubstr: "restart failed",
		},
		{
			name: "health failure triggers rollback",
			setup: func(t *testing.T, s *store.Store) (int64, string, string) {
				issueUser(t, ctx, s, 999, "user_999", "0000000000000000000000000000000a")
				return 999, "user_999", "0000000000000000000000000000000a"
			},
			action: func(a *Applier, tgID int64, profileName, secret string) error {
				_, err := a.IssueProfile(ctx, tgID, profileName, secret)
				return err
			},
			runner: func() *fakeRunner {
				r := &fakeRunner{serviceOut: "active\n"}
				return r
			},
			wantErr:   ErrNotReady,
			wantApply: 2, // apply + rollback apply
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testDir := filepath.Join(dir, tt.name)
			if err := os.MkdirAll(testDir, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := WriteProfiles(filepath.Join(testDir, "profiles.json"), seed); err != nil {
				t.Fatal(err)
			}

			s := openTestStore(t)
			tgID, profileName, secret := tt.setup(t, s)
			runner := tt.runner()

			var readyzURL string
			if tt.name == "health failure triggers rollback" {
				notReady := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusServiceUnavailable)
				}))
				defer notReady.Close()
				readyzURL = notReady.URL
			} else {
				readyzURL = readyServer(t)
			}

			a := newTestApplier(t, Store{s}, readyzURL, runner, testDir)

			if tt.name == "validation failure" {
				bin := filepath.Join(testDir, "tproxy-server-check")
				if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
					t.Fatal(err)
				}
				a.cfg.TproxyServerBin = bin
			}

			err := tt.action(a, tgID, profileName, secret)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("expected %v, got %v", tt.wantErr, err)
				}
				if tt.errSubstr != "" && !strings.Contains(err.Error(), tt.errSubstr) {
					t.Errorf("error %q missing %q", err.Error(), tt.errSubstr)
				}
			} else if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(runner.applyCalls) != tt.wantApply {
				t.Errorf("apply calls = %d, want %d", len(runner.applyCalls), tt.wantApply)
			}
			if tt.wantCheck > 0 && len(runner.checkCalls) != tt.wantCheck {
				t.Errorf("check calls = %d, want %d", len(runner.checkCalls), tt.wantCheck)
			}
		})
	}
}

func TestIssueProfile_Success(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	issueUser(t, ctx, s, 555, "user_555", "cccccccccccccccccccccccccccccccc")

	runner := &fakeRunner{}
	dir := t.TempDir()
	if err := WriteProfiles(filepath.Join(dir, "profiles.json"), &ProfilesFile{}); err != nil {
		t.Fatal(err)
	}
	a := newTestApplier(t, Store{s}, readyServer(t), runner, dir)

	res, err := a.IssueProfile(ctx, 555, "user_555", "cccccccccccccccccccccccccccccccc")
	if err != nil {
		t.Fatalf("IssueProfile() error = %v", err)
	}
	if res.ProfileCount != 1 {
		t.Errorf("ProfileCount = %d, want 1", res.ProfileCount)
	}
	if len(runner.applyCalls) != 1 || runner.applyCalls[0][1] != res.CandidatePath {
		t.Errorf("apply args = %v, candidate = %s", runner.applyCalls, res.CandidatePath)
	}
}

func TestRevokeProfile_StillActiveMismatch(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	issueUser(t, ctx, s, 666, "user_666", "dddddddddddddddddddddddddddddddd")

	runner := &fakeRunner{}
	a := newTestApplier(t, Store{s}, readyServer(t), runner, t.TempDir())

	_, err := a.RevokeProfile(ctx, 666)
	if !errors.Is(err, ErrStateMismatch) {
		t.Fatalf("expected ErrStateMismatch, got %v", err)
	}
}

func TestRevokeProfile_Success(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	issueUser(t, ctx, s, 666, "user_666", "dddddddddddddddddddddddddddddddd")
	revokeUser(t, ctx, s, 666)

	runner := &fakeRunner{}
	dir := t.TempDir()
	if err := WriteProfiles(filepath.Join(dir, "profiles.json"), &ProfilesFile{}); err != nil {
		t.Fatal(err)
	}
	a := newTestApplier(t, Store{s}, readyServer(t), runner, dir)

	res, err := a.RevokeProfile(ctx, 666)
	if err != nil {
		t.Fatalf("RevokeProfile() error = %v", err)
	}
	if res.ProfileCount != 0 {
		t.Errorf("ProfileCount = %d, want 0", res.ProfileCount)
	}
}

// Ensure Store satisfies storeIface at compile time.
var _ storeIface = Store{}

// Ensure revoked status constant is used.
var _ = models.StatusRevoked
