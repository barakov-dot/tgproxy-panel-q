// Package bot implements tgproxy-panel's Telegram bot: long polling,
// /start, the "get proxy" button, and the admin approve/deny flow
// (plan.md §6).
//
// This file holds the orchestration core (Actions) deliberately kept free
// of any tgbotapi type, so it can be unit-tested without a live Telegram
// connection and, per CLAUDE.md's stage-6 plan, lifted into a shared
// internal/service package alongside httpserver's equivalent logic. See the
// doc comment on Actions for the consolidation note.
package bot

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/barakov-dot/tgproxy-panel/internal/applier"
	"github.com/barakov-dot/tgproxy-panel/internal/models"
	"github.com/barakov-dot/tgproxy-panel/internal/secretgen"
	"github.com/barakov-dot/tgproxy-panel/internal/store"
)

// ErrApplyFailed means the DB was updated to reflect an issued profile but
// internal/applier failed to push that change to the host (see
// applier.IssueProfile's doc comment on the DB-write-then-apply ordering
// contract). Callers must not treat the operation as fully successful when
// they see this, but the DB row is left as-is rather than rolled back —
// same rationale as internal/applier's own ErrNotReady doc comment.
var ErrApplyFailed = errors.New("bot: db updated but applier failed to apply the change on the host")

const (
	actorAutoIssue = "bot:auto_issue"
)

func actorAdmin(adminTelegramID int64) string {
	return "bot:admin:" + strconv.FormatInt(adminTelegramID, 10)
}

// Store is the subset of *store.Store the bot's orchestration needs.
// *store.Store satisfies this directly.
type Store interface {
	GetUserByTelegramID(ctx context.Context, telegramID int64) (*models.User, error)
	CreateUser(ctx context.Context, telegramID int64, username, firstName, lastName *string) (*models.User, error)
	IssueUser(ctx context.Context, telegramID int64, profileName, secret string) (*models.User, error)
	DenyUser(ctx context.Context, telegramID int64) (*models.User, error)
	GetSetting(ctx context.Context, key string) (string, bool, error)
	WriteAuditLog(ctx context.Context, entry models.AuditLog) error
}

// Applier is the subset of *applier.Applier the bot's orchestration needs.
// *applier.Applier satisfies this directly.
type Applier interface {
	IssueProfile(ctx context.Context, telegramID int64, profileName, secret string) (*applier.Result, error)
}

// Actions holds the framework-agnostic issue/approve/deny orchestration the
// bot needs. It takes plain Store/Applier dependencies and involves no
// Telegram types — only the handlers in bot.go deal with chat IDs, updates
// and keyboards.
//
// Consolidation note (CLAUDE.md stage 6, plan.md §6's "та же логика
// выдачи/отказа, что и в панели" requirement): internal/httpserver is being
// built concurrently by a different agent against the identical instruction,
// and very likely wrote its own near-duplicate issue/approve/deny
// orchestration since internal/service does not exist yet. This file (plus
// Approve/Deny/RequestProxy below) is the bot's half of that duplication,
// intentionally written with no Telegram dependency so it can be lifted
// almost as-is. Stage 6 should: (1) diff this file against httpserver's
// equivalent (likely internal/httpserver/actions.go or similar), (2) move
// the more complete/correct version's Store/Applier interfaces and
// issue/approve/deny functions into a new internal/service package, (3)
// replace this file's body with thin calls into internal/service, keeping
// only the RequestOutcome/RequestResult types and the auto-issue-check
// wrapper (RequestProxy) here since that part is bot-specific (httpserver
// has no equivalent "user pressed a button" entry point).
type Actions struct {
	Store   Store
	Applier Applier

	// DefaultAutoIssue is used when the settings table has no auto_issue
	// row yet (e.g. before anyone has ever opened the panel's settings
	// page) — seeded from config.Config.AutoIssue at startup.
	DefaultAutoIssue bool

	// GenerateSecret and ProfileName default to internal/secretgen's
	// package functions; overridable so tests can use deterministic
	// values instead of crypto/rand.
	GenerateSecret func() (string, error)
	ProfileName    func(telegramID int64) string
}

// NewActions builds an Actions with production defaults (real secretgen
// functions).
func NewActions(st Store, ap Applier, defaultAutoIssue bool) *Actions {
	return &Actions{
		Store:            st,
		Applier:          ap,
		DefaultAutoIssue: defaultAutoIssue,
		GenerateSecret:   secretgen.GenerateSecret,
		ProfileName:      secretgen.ProfileName,
	}
}

// RequestOutcome describes what RequestProxy did in response to a user
// pressing "get proxy".
type RequestOutcome int

const (
	// OutcomeAlreadyActive means the user already had an active profile;
	// nothing was changed, the existing link should be resent.
	OutcomeAlreadyActive RequestOutcome = iota
	// OutcomeIssued means a new profile was generated and applied; the new
	// link should be sent.
	OutcomeIssued
	// OutcomePendingCreated means auto-issue is off and a request was
	// recorded; the admin should be notified and the user told to wait.
	OutcomePendingCreated
	// OutcomeAlreadyPending means the user already had a request awaiting
	// review; nothing new was created.
	OutcomeAlreadyPending
)

// RequestResult is RequestProxy's outcome plus the resulting user row.
type RequestResult struct {
	Outcome RequestOutcome
	User    *models.User
}

// RequestProxy implements the "user pressed 🔑 Получить прокси" flow
// (plan.md §6): look up any existing request, and either report the
// existing profile, issue a new one (auto-issue on), or record a pending
// request (auto-issue off).
//
// Known gap: for a user re-requesting after a prior revoke/deny, with
// auto-issue off, there is no store method to reset an existing row back to
// status=pending (store.CreateUser only inserts, and would violate
// telegram_id's UNIQUE constraint here) — internal/store is out of this
// package's scope to extend. The admin is still notified and can approve/
// deny as usual; only the DB's status column lags behind (stays
// revoked/denied) until acted on. A future internal/service consolidation
// should add a store method for this if it matters for the panel's pending
// list.
func (a *Actions) RequestProxy(ctx context.Context, telegramID int64, username, firstName, lastName *string) (*RequestResult, error) {
	u, err := a.Store.GetUserByTelegramID(ctx, telegramID)
	switch {
	case err == nil:
		switch u.Status {
		case models.StatusActive:
			return &RequestResult{Outcome: OutcomeAlreadyActive, User: u}, nil
		case models.StatusPending:
			return &RequestResult{Outcome: OutcomeAlreadyPending, User: u}, nil
		}
		// revoked or denied: fall through and treat like a fresh request.
	case errors.Is(err, store.ErrNotFound):
		u, err = a.Store.CreateUser(ctx, telegramID, username, firstName, lastName)
		if err != nil {
			return nil, fmt.Errorf("bot: create user telegram_id=%d: %w", telegramID, err)
		}
	default:
		return nil, fmt.Errorf("bot: get user telegram_id=%d: %w", telegramID, err)
	}

	autoIssue, err := a.autoIssueEnabled(ctx)
	if err != nil {
		return nil, err
	}
	if !autoIssue {
		return &RequestResult{Outcome: OutcomePendingCreated, User: u}, nil
	}

	issued, err := a.issue(ctx, telegramID, actorAutoIssue)
	if err != nil {
		return nil, err
	}
	return &RequestResult{Outcome: OutcomeIssued, User: issued}, nil
}

// Approve issues a profile for telegramID on behalf of adminTelegramID,
// using the same DB-write-then-applier-call flow as auto-issue.
func (a *Actions) Approve(ctx context.Context, telegramID, adminTelegramID int64) (*models.User, error) {
	return a.issue(ctx, telegramID, actorAdmin(adminTelegramID))
}

// Deny marks telegramID's request denied on behalf of adminTelegramID.
func (a *Actions) Deny(ctx context.Context, telegramID, adminTelegramID int64) (*models.User, error) {
	u, err := a.Store.DenyUser(ctx, telegramID)
	if err != nil {
		return nil, fmt.Errorf("bot: deny user telegram_id=%d: %w", telegramID, err)
	}
	a.audit(ctx, "deny", telegramID, actorAdmin(adminTelegramID), "")
	return u, nil
}

// issue is the shared DB-write-then-apply flow used by both auto-issue and
// admin approval: write the DB first, then call the applier, exactly per
// applier.IssueProfile's ordering contract.
func (a *Actions) issue(ctx context.Context, telegramID int64, actor string) (*models.User, error) {
	secret, err := a.generateSecret()
	if err != nil {
		return nil, fmt.Errorf("bot: generate secret: %w", err)
	}
	name := a.profileName(telegramID)

	u, err := a.Store.IssueUser(ctx, telegramID, name, secret)
	if err != nil {
		return nil, fmt.Errorf("bot: issue user telegram_id=%d: %w", telegramID, err)
	}

	if _, err := a.Applier.IssueProfile(ctx, telegramID, name, secret); err != nil {
		a.audit(ctx, "issue_apply_failed", telegramID, actor, err.Error())
		return nil, fmt.Errorf("%w: telegram_id=%d: %v", ErrApplyFailed, telegramID, err)
	}

	a.audit(ctx, "issue", telegramID, actor, "profile_name="+name)
	return u, nil
}

func (a *Actions) audit(ctx context.Context, action string, telegramID int64, actor, detail string) {
	id := telegramID
	// Best-effort: a failed audit write must not fail the user-facing
	// operation that already succeeded (or already failed for its own
	// reason) by this point.
	_ = a.Store.WriteAuditLog(ctx, models.AuditLog{Action: action, TelegramID: &id, Actor: actor, Detail: detail})
}

func (a *Actions) autoIssueEnabled(ctx context.Context) (bool, error) {
	v, ok, err := a.Store.GetSetting(ctx, models.SettingAutoIssue)
	if err != nil {
		return false, fmt.Errorf("bot: get auto_issue setting: %w", err)
	}
	if !ok {
		return a.DefaultAutoIssue, nil
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return false, fmt.Errorf("bot: parse auto_issue setting %q: %w", v, err)
	}
	return b, nil
}

func (a *Actions) generateSecret() (string, error) {
	if a.GenerateSecret != nil {
		return a.GenerateSecret()
	}
	return secretgen.GenerateSecret()
}

func (a *Actions) profileName(telegramID int64) string {
	if a.ProfileName != nil {
		return a.ProfileName(telegramID)
	}
	return secretgen.ProfileName(telegramID)
}
