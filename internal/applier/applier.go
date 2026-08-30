package applier

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/barakov-dot/tgproxy-panel-q/internal/config"
	"github.com/barakov-dot/tgproxy-panel-q/internal/models"
	"github.com/barakov-dot/tgproxy-panel-q/internal/store"
)

var (
	ErrStateMismatch    = errors.New("applier: db state does not match expected value")
	ErrValidationFailed = errors.New("applier: candidate profiles.json failed validation")
	ErrApplyScriptFailed = errors.New("applier: apply-profiles.sh failed")
	ErrNotReady         = errors.New("applier: tproxy-server did not become ready after restart")
	ErrRollbackFailed   = errors.New("applier: rollback failed")
)

type storeIface interface {
	userLister
	GetUserByTelegramID(ctx context.Context, telegramID int64) (*models.User, error)
}

// Applier pushes desired profiles.json state to the host.
type Applier struct {
	cfg   *config.Config
	store storeIface

	runner     Runner
	httpClient *http.Client

	healthCheckAttempts int
	healthCheckInterval time.Duration
}

// Store wraps *store.Store for use with New.
type Store struct {
	*store.Store
}

func (s Store) ListUsers(ctx context.Context) ([]*models.User, error) {
	return s.Store.ListUsers(ctx, store.UserListFilter{}, store.UserListSort{})
}

// New builds an Applier for production use.
func New(cfg *config.Config, s Store) *Applier {
	return &Applier{
		cfg:                 cfg,
		store:               s,
	 runner:              execRunner{},
		httpClient:          &http.Client{Timeout: 5 * time.Second},
		healthCheckAttempts: defaultHealthCheckAttempts,
		healthCheckInterval: defaultHealthCheckInterval,
	}
}

// Result describes a successful apply (no secrets).
type Result struct {
	CandidatePath  string
	ProfileCount   int
	ApplyStdout    string
	ApplyStderr    string
	HealthAttempts int
	RolledBack     bool
}

// IssueProfile applies after the DB records an active profile for telegramID.
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

// RevokeProfile applies after the DB records a revoked user.
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

func (a *Applier) apply(ctx context.Context) (*Result, error) {
	pf, err := desiredProfiles(ctx, a.store, a.cfg)
	if err != nil {
		return nil, err
	}

	candidatePath, err := writeCandidate(a.cfg.BackupDir, pf, a.cfg.BackupKeep)
	if err != nil {
		return nil, err
	}

	if err := a.validateCandidate(ctx, candidatePath); err != nil {
		return nil, err
	}

	if _, err := backupProfiles(a.cfg.TproxyProfilesPath, a.cfg.BackupDir, a.cfg.BackupKeep); err != nil {
		if !isUnreadableProfiles(err) {
			return nil, err
		}
	}

	stdout, stderr, err := a.runApplyScript(ctx, candidatePath)
	if err != nil {
		return nil, fmt.Errorf("%w: %v: %s", ErrApplyScriptFailed, err, stderr)
	}

	if err := waitForHealthy(ctx, a.runner, a.httpClient, a.cfg, a.healthCheckAttempts, a.healthCheckInterval); err != nil {
		rolledBack, rbErr := a.rollback(ctx)
		if rbErr != nil {
			return nil, fmt.Errorf("%w: health check: %v; rollback: %v", ErrNotReady, err, rbErr)
		}
		if rolledBack {
			return nil, fmt.Errorf("%w: %v (rolled back to previous profiles)", ErrNotReady, err)
		}
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

func (a *Applier) validateCandidate(ctx context.Context, candidatePath string) error {
	pf, err := ReadProfiles(candidatePath)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrValidationFailed, err)
	}

	// Panel-only candidates may be empty after revoke; apply-profiles.sh merges
	// them with non-panel profiles (e.g. upstream "default") before -check.
	if len(pf.Profiles) == 0 {
		return nil
	}

	if a.cfg.TproxyServerBin == "" {
		return nil
	}
	if _, err := os.Stat(a.cfg.TproxyServerBin); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("applier: stat tproxy-server binary: %w", err)
	}

	if _, stderr, err := a.runner.Run(ctx, a.cfg.TproxyServerBin,
		"-config", a.cfg.TproxyConfigPath, "-profiles-file", candidatePath, "-check"); err != nil {
		return fmt.Errorf("%w: %v: %s", ErrValidationFailed, err, stderr)
	}
	return nil
}

func (a *Applier) runApplyScript(ctx context.Context, candidatePath string) (stdout, stderr string, err error) {
	if os.Getenv("RUN_AS_ROOT") == "1" {
		return a.runner.Run(ctx, a.cfg.ApplyProfilesScript, candidatePath)
	}
	return a.runner.Run(ctx, "sudo", a.cfg.ApplyProfilesScript, candidatePath)
}

func (a *Applier) rollback(ctx context.Context) (bool, error) {
	backupPath, err := latestBackup(a.cfg.BackupDir)
	if err != nil {
		return false, fmt.Errorf("%w: find backup: %v", ErrRollbackFailed, err)
	}

	if _, stderr, err := a.runApplyScript(ctx, backupPath); err != nil {
		return false, fmt.Errorf("%w: %v: %s", ErrRollbackFailed, err, stderr)
	}

	if err := waitForHealthy(ctx, a.runner, a.httpClient, a.cfg, a.healthCheckAttempts, a.healthCheckInterval); err != nil {
		return true, fmt.Errorf("%w: post-rollback health: %v", ErrRollbackFailed, err)
	}
	return true, nil
}

func isUnreadableProfiles(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, os.ErrPermission) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "permission denied") || strings.Contains(msg, "operation not permitted")
}
