package applier

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/barakov-dot/tgproxy-panel/internal/config"
	"github.com/barakov-dot/tgproxy-panel/internal/models"
)

// Sentinel errors identifying which stage of an apply failed, so a future
// internal/service layer can decide what to do (e.g. whether a rollback is
// worth attempting) without string-matching error text.
var (
	// ErrStateMismatch means the DB row this call expected to already
	// reflect (profile_name/secret/status) doesn't match what was passed
	// in — a sign the caller's write ordering is wrong (see IssueProfile's
	// doc comment) or a concurrent modification happened.
	ErrStateMismatch = errors.New("applier: db state does not match expected value")

	// ErrValidationFailed means tproxy-server's own `-check` rejected the
	// candidate profiles.json. Nothing was handed to apply-profiles.sh, so
	// the live host state is untouched — no rollback needed.
	ErrValidationFailed = errors.New("applier: candidate profiles.json failed -check validation")

	// ErrApplyScriptFailed means `sudo apply-profiles.sh <candidate>`
	// exited non-zero. The rollback contract we expect from that future
	// script: apply-profiles.sh backs up the live profiles.json *before*
	// overwriting it, and must leave the live file and the service
	// untouched (not restarted) if anything after the backup fails, so a
	// non-zero exit here means the host is still running the previous,
	// known-good configuration — no rollback action is needed on our side
	// either. If that invariant ever changes, this comment and the
	// service-layer rollback logic built on top of it both need updating.
	ErrApplyScriptFailed = errors.New("applier: apply-profiles.sh failed")

	// ErrNotReady means apply-profiles.sh reported success (it already
	// restarted tproxy-server itself, see CLAUDE.md's directory-structure
	// bullet for that script) but /readyz never returned 200 within the
	// retry budget. Unlike the two errors above, this is the one case
	// where the *live* file has already changed — a caller wanting to
	// roll back needs apply-profiles.sh's cooperation to restore its own
	// backup, which is out of scope for this stage (no such script exists
	// yet); for now we only guarantee the DB is left untouched by us, and
	// the service layer must not flip a user to active/revoked when it
	// sees this error.
	ErrNotReady = errors.New("applier: tproxy-server did not become ready after restart")
)

// storeIface is the subset of *store.Store this package needs: read the
// desired state (userLister, in desired.go) plus look up one user for the
// consistency check in Issue/RevokeProfile.
type storeIface interface {
	userLister
	GetUserByTelegramID(ctx context.Context, telegramID int64) (*models.User, error)
}

// Applier is the only type in the codebase allowed to shell out to
// sudo/tproxy-server/systemctl-adjacent tooling or poll the tproxy-server
// admin API. See the package doc comment in profiles.go for the
// declarative, DB-is-source-of-truth design this is built on.
type Applier struct {
	cfg   *config.Config
	store storeIface

	runner     commandRunner
	httpClient *http.Client

	healthCheckAttempts int
	healthCheckInterval time.Duration
}

// New builds an Applier for production use: real exec.Command calls, a
// real HTTP client, and the retry timing matched to tproxy-server's own
// install.sh (20 attempts, 1s apart).
func New(cfg *config.Config, s storeIface) *Applier {
	return &Applier{
		cfg:                 cfg,
		store:               s,
		runner:              execCommandRunner{},
		httpClient:          &http.Client{Timeout: 5 * time.Second},
		healthCheckAttempts: defaultHealthCheckAttempts,
		healthCheckInterval: defaultHealthCheckInterval,
	}
}

// Result describes the outcome of a successful apply, for the service
// layer to log/audit. It carries no secrets.
type Result struct {
	CandidatePath  string
	ProfileCount   int
	ApplyStdout    string
	ApplyStderr    string
	HealthAttempts int
}

// IssueProfile makes the host match the DB's desired state after a new
// active profile has been recorded.
//
// Ordering contract with the future internal/service layer: this method
// does NOT call store.IssueUser — per CLAUDE.md's package-boundary
// convention, DB orchestration belongs to internal/service, not here.
// service must call store.IssueUser(telegramID, profileName, secret)
// *before* calling IssueProfile, then call this to push the resulting
// desired state to the host. profileName/secret are passed in purely as a
// consistency check (protects against a caller bug or a race clobbering
// the row between the DB write and this call) — the actual desired list
// handed to the host is always recomputed fresh from every active user in
// the DB, this one included.
func (a *Applier) IssueProfile(ctx context.Context, telegramID int64, profileName, secret string) (*Result, error) {
	u, err := a.store.GetUserByTelegramID(ctx, telegramID)
	if err != nil {
		return nil, fmt.Errorf("applier: issue profile telegram_id=%d: %w", telegramID, err)
	}
	if u.Status != models.StatusActive || u.ProfileName == nil || *u.ProfileName != profileName ||
		u.Secret == nil || *u.Secret != secret {
		return nil, fmt.Errorf("%w: telegram_id=%d expected active profile %q, db has status=%s",
			ErrStateMismatch, telegramID, profileName, u.Status)
	}
	return a.apply(ctx)
}

// RevokeProfile makes the host match the DB's desired state after a
// profile has been revoked. Same ordering contract as IssueProfile: call
// store.RevokeUser first, then this.
func (a *Applier) RevokeProfile(ctx context.Context, telegramID int64) (*Result, error) {
	u, err := a.store.GetUserByTelegramID(ctx, telegramID)
	if err != nil {
		return nil, fmt.Errorf("applier: revoke profile telegram_id=%d: %w", telegramID, err)
	}
	if u.Status != models.StatusRevoked {
		return nil, fmt.Errorf("%w: telegram_id=%d expected status=revoked, db has status=%s",
			ErrStateMismatch, telegramID, u.Status)
	}
	return a.apply(ctx)
}

// apply runs the full flow: compute desired state, stage a candidate file,
// validate it with tproxy-server -check, hand it to apply-profiles.sh via
// sudo, then poll /readyz.
func (a *Applier) apply(ctx context.Context) (*Result, error) {
	pf, err := desiredProfiles(ctx, a.store, a.cfg)
	if err != nil {
		return nil, err
	}

	candidatePath, err := writeCandidate(a.cfg.BackupDir, pf, a.cfg.BackupKeep)
	if err != nil {
		return nil, err
	}

	if _, stderr, err := a.runner.Run(ctx, a.cfg.TproxyServerBin,
		"-config", a.cfg.TproxyConfigPath, "-profiles-file", candidatePath, "-check"); err != nil {
		return nil, fmt.Errorf("%w: %v: %s", ErrValidationFailed, err, stderr)
	}

	stdout, stderr, err := a.runner.Run(ctx, "sudo", a.cfg.ApplyProfilesScript, candidatePath)
	if err != nil {
		return nil, fmt.Errorf("%w: %v: %s", ErrApplyScriptFailed, err, stderr)
	}

	if err := waitForReady(ctx, a.httpClient, a.cfg.TproxyAdminURL, a.healthCheckAttempts, a.healthCheckInterval); err != nil {
		return nil, err
	}

	return &Result{
		CandidatePath:  candidatePath,
		ProfileCount:   len(pf.Profiles),
		ApplyStdout:    stdout,
		ApplyStderr:    stderr,
		HealthAttempts: a.healthCheckAttempts,
	}, nil
}
