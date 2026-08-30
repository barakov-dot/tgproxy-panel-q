// Package service is the shared issue/revoke/approve/deny/request
// orchestration used by both internal/httpserver and internal/bot
// (plan.md §6's "та же логика выдачи/отказа, что и в панели" requirement —
// the bot's approve/deny must be the same logic as the panel's, not a
// reimplementation). It is the only layer allowed to sequence a
// Store write together with an Applier call for a state-changing operation;
// callers only build request-specific plumbing (HTTP handlers/templates,
// Telegram messages/keyboards) around it.
//
// This package was consolidated from two independently-built
// implementations (internal/httpserver/actions.go and
// internal/bot/actions.go, written concurrently before this package
// existed) — Approve/Revoke/Deny came from the httpserver version (it had
// the more complete DB-rollback-on-apply-failure handling), RequestProxy
// came from the bot version, and RequestProxy's pending-request handling
// was extended to use store.SetPending so a revoked/denied user re-
// requesting with auto-issue off reappears in the pending queue instead of
// silently staying revoked/denied.
package service

import (
	"context"

	"github.com/barakov-dot/tgproxy-panel/internal/applier"
	"github.com/barakov-dot/tgproxy-panel/internal/models"
	"github.com/barakov-dot/tgproxy-panel/internal/secretgen"
)

// Store is the subset of *store.Store this package needs. *store.Store
// satisfies this directly; tests can substitute a small in-memory fake.
type Store interface {
	GetUserByID(ctx context.Context, id int64) (*models.User, error)
	GetUserByTelegramID(ctx context.Context, telegramID int64) (*models.User, error)
	CreateUser(ctx context.Context, telegramID int64, username, firstName, lastName *string) (*models.User, error)
	ListUsers(ctx context.Context) ([]*models.User, error)
	IssueUser(ctx context.Context, telegramID int64, profileName, secret string) (*models.User, error)
	RevokeUser(ctx context.Context, telegramID int64) (*models.User, error)
	DenyUser(ctx context.Context, telegramID int64) (*models.User, error)
	SetPending(ctx context.Context, telegramID int64) (*models.User, error)
	GetSetting(ctx context.Context, key string) (string, bool, error)
	SetSetting(ctx context.Context, key, value string) error
	WriteAuditLog(ctx context.Context, entry models.AuditLog) error
}

// Applier is the subset of *applier.Applier this package needs.
type Applier interface {
	IssueProfile(ctx context.Context, telegramID int64, profileName, secret string) (*applier.Result, error)
	RevokeProfile(ctx context.Context, telegramID int64) (*applier.Result, error)
}

// Actions holds the framework-agnostic issue/revoke/approve/deny/request
// orchestration. Both internal/httpserver and internal/bot hold one of
// these and call into it rather than touching Store/Applier directly for
// any state-changing operation.
type Actions struct {
	Store   Store
	Applier Applier

	// DefaultAutoIssue seeds RequestProxy's auto-issue check when the
	// settings table has no auto_issue row yet (before anyone has opened
	// the panel's settings page) — from config.Config.AutoIssue at
	// startup.
	DefaultAutoIssue bool

	// GenSecret and ProfileName default to internal/secretgen's package
	// functions; overridable so tests can use deterministic values instead
	// of crypto/rand.
	GenSecret   func() (string, error)
	ProfileName func(telegramID int64) string
}

// New builds an Actions with production defaults (real secretgen
// functions).
func New(store Store, ap Applier, defaultAutoIssue bool) *Actions {
	return &Actions{
		Store:            store,
		Applier:          ap,
		DefaultAutoIssue: defaultAutoIssue,
		GenSecret:        secretgen.GenerateSecret,
		ProfileName:      secretgen.ProfileName,
	}
}

func (a *Actions) genSecret() (string, error) {
	if a.GenSecret != nil {
		return a.GenSecret()
	}
	return secretgen.GenerateSecret()
}

func (a *Actions) profileName(telegramID int64) string {
	if a.ProfileName != nil {
		return a.ProfileName(telegramID)
	}
	return secretgen.ProfileName(telegramID)
}

func (a *Actions) audit(ctx context.Context, action string, telegramID int64, actor, detail string) {
	id := telegramID
	// Best-effort: a failed audit write must not fail the user-facing
	// operation that already succeeded (or already failed for its own
	// reason) by this point.
	_ = a.Store.WriteAuditLog(ctx, models.AuditLog{Action: action, TelegramID: &id, Actor: actor, Detail: detail})
}
