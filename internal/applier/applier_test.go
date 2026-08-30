package applier

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/barakov-dot/tgproxy-panel/internal/config"
)

// fakeRunner substitutes for execCommandRunner in tests: no real
// tproxy-server binary or sudo exists on the macOS dev/test machine.
type fakeRunner struct {
	checkErr    error
	checkStderr string
	checkCalls  [][]string

	applyErr    error
	applyStdout string
	applyStderr string
	applyCalls  [][]string
}

func (f *fakeRunner) Run(ctx context.Context, name string, args ...string) (string, string, error) {
	if strings.Contains(name, "sudo") {
		f.applyCalls = append(f.applyCalls, args)
		return f.applyStdout, f.applyStderr, f.applyErr
	}
	f.checkCalls = append(f.checkCalls, args)
	return "", f.checkStderr, f.checkErr
}

func newTestApplier(t *testing.T, s storeIface, readyzURL string, runner *fakeRunner) *Applier {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.Config{
		TproxyBackend:       "127.0.0.1:2398",
		TproxyCarrierMode:   "https",
		TproxyConfigPath:    "/etc/tproxy-server/config.json",
		TproxyServerBin:     "/usr/local/bin/tproxy-server",
		TproxyAdminURL:      readyzURL,
		ApplyProfilesScript: "/opt/tgproxy-panel/bin/apply-profiles.sh",
		BackupDir:           dir,
		BackupKeep:          100,
	}
	return &Applier{
		cfg:                 cfg,
		store:               s,
		runner:              runner,
		httpClient:          &http.Client{Timeout: time.Second},
		healthCheckAttempts: 3,
		healthCheckInterval: time.Millisecond,
	}
}

func readyServer(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestIssueProfile_Success(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	if _, err := s.CreateUser(ctx, 555, nil, nil, nil); err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}
	if _, err := s.IssueUser(ctx, 555, "user_555", "cccccccccccccccccccccccccccccccc"); err != nil {
		t.Fatalf("IssueUser() error = %v", err)
	}

	runner := &fakeRunner{}
	a := newTestApplier(t, s, readyServer(t), runner)

	res, err := a.IssueProfile(ctx, 555, "user_555", "cccccccccccccccccccccccccccccccc")
	if err != nil {
		t.Fatalf("IssueProfile() error = %v", err)
	}
	if res.ProfileCount != 1 {
		t.Errorf("ProfileCount = %d, want 1", res.ProfileCount)
	}
	if len(runner.checkCalls) != 1 {
		t.Fatalf("expected 1 -check call, got %d", len(runner.checkCalls))
	}
	if len(runner.applyCalls) != 1 {
		t.Fatalf("expected 1 apply call, got %d", len(runner.applyCalls))
	}
	// apply-profiles.sh must be invoked with the candidate path as its
	// sole argument.
	if len(runner.applyCalls[0]) != 2 || runner.applyCalls[0][1] != res.CandidatePath {
		t.Errorf("unexpected apply args: %v (candidate=%s)", runner.applyCalls[0], res.CandidatePath)
	}
}

func TestIssueProfile_StateMismatch_WrongProfileName(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	if _, err := s.CreateUser(ctx, 555, nil, nil, nil); err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}
	if _, err := s.IssueUser(ctx, 555, "user_555", "cccccccccccccccccccccccccccccccc"); err != nil {
		t.Fatalf("IssueUser() error = %v", err)
	}

	runner := &fakeRunner{}
	a := newTestApplier(t, s, readyServer(t), runner)

	_, err := a.IssueProfile(ctx, 555, "wrong_name", "cccccccccccccccccccccccccccccccc")
	if !errors.Is(err, ErrStateMismatch) {
		t.Fatalf("expected ErrStateMismatch, got %v", err)
	}
	if len(runner.checkCalls) != 0 || len(runner.applyCalls) != 0 {
		t.Errorf("no external command should run on a state mismatch: check=%d apply=%d",
			len(runner.checkCalls), len(runner.applyCalls))
	}
}

func TestIssueProfile_StateMismatch_StillPending(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	if _, err := s.CreateUser(ctx, 555, nil, nil, nil); err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}

	runner := &fakeRunner{}
	a := newTestApplier(t, s, readyServer(t), runner)

	_, err := a.IssueProfile(ctx, 555, "user_555", "secret")
	if !errors.Is(err, ErrStateMismatch) {
		t.Fatalf("expected ErrStateMismatch, got %v", err)
	}
}

func TestRevokeProfile_Success(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	if _, err := s.CreateUser(ctx, 666, nil, nil, nil); err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}
	if _, err := s.IssueUser(ctx, 666, "user_666", "dddddddddddddddddddddddddddddddd"); err != nil {
		t.Fatalf("IssueUser() error = %v", err)
	}
	if _, err := s.RevokeUser(ctx, 666); err != nil {
		t.Fatalf("RevokeUser() error = %v", err)
	}

	runner := &fakeRunner{}
	a := newTestApplier(t, s, readyServer(t), runner)

	res, err := a.RevokeProfile(ctx, 666)
	if err != nil {
		t.Fatalf("RevokeProfile() error = %v", err)
	}
	if res.ProfileCount != 0 {
		t.Errorf("ProfileCount = %d, want 0", res.ProfileCount)
	}
}

func TestRevokeProfile_StateMismatch_StillActive(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	if _, err := s.CreateUser(ctx, 666, nil, nil, nil); err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}
	if _, err := s.IssueUser(ctx, 666, "user_666", "dddddddddddddddddddddddddddddddd"); err != nil {
		t.Fatalf("IssueUser() error = %v", err)
	}

	runner := &fakeRunner{}
	a := newTestApplier(t, s, readyServer(t), runner)

	_, err := a.RevokeProfile(ctx, 666)
	if !errors.Is(err, ErrStateMismatch) {
		t.Fatalf("expected ErrStateMismatch, got %v", err)
	}
}

func TestApply_ValidationFailureShortCircuits(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	if _, err := s.CreateUser(ctx, 777, nil, nil, nil); err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}
	if _, err := s.IssueUser(ctx, 777, "user_777", "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"); err != nil {
		t.Fatalf("IssueUser() error = %v", err)
	}

	runner := &fakeRunner{checkErr: errors.New("exit status 1"), checkStderr: "profile validation failed"}
	a := newTestApplier(t, s, readyServer(t), runner)

	_, err := a.IssueProfile(ctx, 777, "user_777", "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee")
	if !errors.Is(err, ErrValidationFailed) {
		t.Fatalf("expected ErrValidationFailed, got %v", err)
	}
	if !strings.Contains(err.Error(), "profile validation failed") {
		t.Errorf("error %q should include -check stderr", err.Error())
	}
	if len(runner.applyCalls) != 0 {
		t.Errorf("apply-profiles.sh must not run when -check fails, got %d calls", len(runner.applyCalls))
	}
}

func TestApply_ScriptFailure(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	if _, err := s.CreateUser(ctx, 888, nil, nil, nil); err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}
	if _, err := s.IssueUser(ctx, 888, "user_888", "ffffffffffffffffffffffffffffffff"); err != nil {
		t.Fatalf("IssueUser() error = %v", err)
	}

	runner := &fakeRunner{applyErr: errors.New("exit status 1"), applyStderr: "backup failed"}
	a := newTestApplier(t, s, readyServer(t), runner)

	_, err := a.IssueProfile(ctx, 888, "user_888", "ffffffffffffffffffffffffffffffff")
	if !errors.Is(err, ErrApplyScriptFailed) {
		t.Fatalf("expected ErrApplyScriptFailed, got %v", err)
	}
	if !strings.Contains(err.Error(), "backup failed") {
		t.Errorf("error %q should include apply-profiles.sh stderr", err.Error())
	}
}

func TestApply_HealthCheckFailure(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	if _, err := s.CreateUser(ctx, 999, nil, nil, nil); err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}
	if _, err := s.IssueUser(ctx, 999, "user_999", "0000000000000000000000000000000a"); err != nil {
		t.Fatalf("IssueUser() error = %v", err)
	}

	notReady := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer notReady.Close()

	runner := &fakeRunner{}
	a := newTestApplier(t, s, notReady.URL, runner)

	_, err := a.IssueProfile(ctx, 999, "user_999", "0000000000000000000000000000000a")
	if !errors.Is(err, ErrNotReady) {
		t.Fatalf("expected ErrNotReady, got %v", err)
	}
	// The apply script did already run (host state may have changed) —
	// this is the one case documented in ErrNotReady's comment where a
	// rollback would need apply-profiles.sh's cooperation.
	if len(runner.applyCalls) != 1 {
		t.Errorf("expected apply-profiles.sh to have run before the health check, got %d calls", len(runner.applyCalls))
	}
}
