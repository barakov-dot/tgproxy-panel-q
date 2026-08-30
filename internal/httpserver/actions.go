// Actions in this file implement "issue/approve", "revoke" and "deny" as
// framework-agnostic orchestration over userStore + profileApplier + secret
// generation. CLAUDE.md reserves this exact orchestration for a shared
// internal/service package (stage 6), used by both internal/httpserver and
// internal/bot so the two never duplicate/diverge on issue/revoke/approve/deny
// logic (plan.md §6). internal/service does not exist yet (this is stage 5,
// built in parallel with internal/bot by a sibling agent), so this file holds
// the logic for now, deliberately written with no *http.Request/http.ResponseWriter
// or template dependency anywhere in it: stage 6 should be able to move this
// file into internal/service near-verbatim (rename the package clause, adjust
// imports, keep the Actions type and its methods and their signatures) rather
// than rederive it. The bot package should NOT reimplement this logic — it
// should wait for stage 6 and call into the lifted internal/service instead.
package httpserver

import (
	"context"
	"errors"
	"fmt"

	"github.com/barakov-dot/tgproxy-panel/internal/models"
	"github.com/barakov-dot/tgproxy-panel/internal/secretgen"
)

var (
	// ErrAlreadyActive is returned by Approve/Issue when the target user
	// already has an active profile — nothing to do.
	ErrAlreadyActive = errors.New("httpserver: user already has an active profile")

	// ErrNotPending is returned by Deny when the target user isn't
	// currently pending.
	ErrNotPending = errors.New("httpserver: user is not pending")

	// ErrNotActive is returned by Revoke when the target user has no
	// active profile to revoke.
	ErrNotActive = errors.New("httpserver: user has no active profile to revoke")

	// ErrIssueFailed wraps an applier failure during Approve/Issue after the
	// DB rollback (best-effort) has been attempted. The caller-facing
	// message should make clear the host was not updated.
	ErrIssueFailed = errors.New("httpserver: issuing the profile on the host failed")

	// ErrRevokeApplyFailed wraps an applier failure during Revoke. Unlike
	// ErrIssueFailed there is no DB rollback here — see Revoke's doc
	// comment for why leaving the DB row revoked is the safer of the two
	// bad options when the host apply itself fails.
	ErrRevokeApplyFailed = errors.New("httpserver: applying the revoke on the host failed")
)

const (
	auditActionIssue           = "issue"
	auditActionIssueRolledBack = "issue_failed_rollback"
	auditActionRevoke          = "revoke"
	auditActionRevokeApplyFail = "revoke_apply_failed"
	auditActionDeny            = "deny"
)

// Actions implements the issue/revoke/approve/deny orchestration described
// in the package doc comment above. GenSecret is overridable for tests;
// production code should leave it nil (New fills in secretgen.GenerateSecret).
type Actions struct {
	Store   userStore
	Applier profileApplier

	// GenSecret defaults to secretgen.GenerateSecret; tests override it to
	// exercise deterministic secrets or forced generation failures.
	GenSecret func() (string, error)
}

// NewActions builds an Actions using the real secretgen.GenerateSecret.
func NewActions(store userStore, ap profileApplier) *Actions {
	return &Actions{Store: store, Applier: ap, GenSecret: secretgen.GenerateSecret}
}

func (a *Actions) genSecret() (string, error) {
	if a.GenSecret != nil {
		return a.GenSecret()
	}
	return secretgen.GenerateSecret()
}

// Approve issues a fresh profile for telegramID: generates a secret and
// profile name, writes the DB row active (store.IssueUser), then pushes the
// resulting desired state to the host (applier.IssueProfile). It is used
// both for approving a pending request and for re-issuing access to a
// previously revoked/denied user — store.IssueUser doesn't care about the
// prior status, and applier.IssueProfile only checks that the row is active
// with the profile name/secret just written.
//
// Ordering matches applier.IssueProfile's documented contract: DB write
// first, then apply. If the apply fails, the DB would be left claiming an
// active profile the host was never actually given — CLAUDE.md explicitly
// calls out that this must never happen, so on apply failure this method
// attempts to roll the DB row back (store.RevokeUser, which also clears
// profile_name/secret) before returning ErrIssueFailed. If even that
// rollback fails, both failures are logged to the audit trail (never
// silently) and the returned error says so explicitly — the caller must
// surface a "partially failed, contact admin" message, not a generic error.
func (a *Actions) Approve(ctx context.Context, telegramID int64, actor string) (*models.User, error) {
	u, err := a.Store.GetUserByTelegramID(ctx, telegramID)
	if err != nil {
		return nil, fmt.Errorf("httpserver: approve telegram_id=%d: %w", telegramID, err)
	}
	if u.Status == models.StatusActive {
		return u, ErrAlreadyActive
	}

	secret, err := a.genSecret()
	if err != nil {
		return nil, fmt.Errorf("httpserver: approve telegram_id=%d: generate secret: %w", telegramID, err)
	}
	profileName := secretgen.ProfileName(telegramID)

	issued, err := a.Store.IssueUser(ctx, telegramID, profileName, secret)
	if err != nil {
		return nil, fmt.Errorf("httpserver: approve telegram_id=%d: db write: %w", telegramID, err)
	}

	if _, applyErr := a.Applier.IssueProfile(ctx, telegramID, profileName, secret); applyErr != nil {
		_, rollbackErr := a.Store.RevokeUser(ctx, telegramID)

		detail := fmt.Sprintf("apply failed for profile_name=%s: %v", profileName, applyErr)
		action := auditActionIssueRolledBack
		if rollbackErr != nil {
			detail += fmt.Sprintf("; DB ROLLBACK ALSO FAILED: %v (db may still say active for a profile the host does not have)", rollbackErr)
		}
		_ = a.Store.WriteAuditLog(ctx, models.AuditLog{
			Action:     action,
			TelegramID: &telegramID,
			Actor:      actor,
			Detail:     detail,
		})

		if rollbackErr != nil {
			return nil, fmt.Errorf("%w: %v (and DB rollback also failed: %v — DB may now incorrectly show this user as active, contact an administrator)",
				ErrIssueFailed, applyErr, rollbackErr)
		}
		return nil, fmt.Errorf("%w: %v", ErrIssueFailed, applyErr)
	}

	_ = a.Store.WriteAuditLog(ctx, models.AuditLog{
		Action:     auditActionIssue,
		TelegramID: &telegramID,
		Actor:      actor,
		Detail:     fmt.Sprintf("profile_name=%s", profileName),
	})
	return issued, nil
}

// Revoke removes telegramID's active profile: writes the DB row revoked
// (store.RevokeUser, which also clears profile_name/secret), then pushes the
// resulting desired state to the host (applier.RevokeProfile).
//
// Note on failure handling, deliberately different from Approve: if the
// apply fails here, the DB already says "revoked" and profile_name/secret
// are already nulled. There is no clean rollback to "active" — restoring
// those columns would mean re-deriving the exact secret/profile_name that
// were just discarded, and there's no guarantee the host's on-disk
// profiles.json still matches what the DB would claim afterwards anyway. Of
// the two possible inconsistent states, "DB says revoked, host might still
// be serving the old secret until the next successful apply" is the safer
// one — the admin isn't told a live grant is now inert when it might not be
// yet, and a retry naturally reconciles once the apply succeeds. So: no
// rollback attempt, just a loud audit entry and a caller-facing error saying
// the host was not confirmed updated.
func (a *Actions) Revoke(ctx context.Context, telegramID int64, actor string) (*models.User, error) {
	u, err := a.Store.GetUserByTelegramID(ctx, telegramID)
	if err != nil {
		return nil, fmt.Errorf("httpserver: revoke telegram_id=%d: %w", telegramID, err)
	}
	if u.Status != models.StatusActive {
		return u, ErrNotActive
	}
	profileName := ""
	if u.ProfileName != nil {
		profileName = *u.ProfileName
	}

	revoked, err := a.Store.RevokeUser(ctx, telegramID)
	if err != nil {
		return nil, fmt.Errorf("httpserver: revoke telegram_id=%d: db write: %w", telegramID, err)
	}

	if _, applyErr := a.Applier.RevokeProfile(ctx, telegramID); applyErr != nil {
		_ = a.Store.WriteAuditLog(ctx, models.AuditLog{
			Action:     auditActionRevokeApplyFail,
			TelegramID: &telegramID,
			Actor:      actor,
			Detail:     fmt.Sprintf("db updated to revoked but host apply failed for former profile_name=%s: %v", profileName, applyErr),
		})
		return nil, fmt.Errorf("%w: %v", ErrRevokeApplyFailed, applyErr)
	}

	_ = a.Store.WriteAuditLog(ctx, models.AuditLog{
		Action:     auditActionRevoke,
		TelegramID: &telegramID,
		Actor:      actor,
		Detail:     fmt.Sprintf("profile_name=%s", profileName),
	})
	return revoked, nil
}

// Deny marks a pending request denied. No applier call: a pending request
// was never applied to the host.
func (a *Actions) Deny(ctx context.Context, telegramID int64, actor string) (*models.User, error) {
	u, err := a.Store.GetUserByTelegramID(ctx, telegramID)
	if err != nil {
		return nil, fmt.Errorf("httpserver: deny telegram_id=%d: %w", telegramID, err)
	}
	if u.Status != models.StatusPending {
		return u, ErrNotPending
	}

	denied, err := a.Store.DenyUser(ctx, telegramID)
	if err != nil {
		return nil, fmt.Errorf("httpserver: deny telegram_id=%d: db write: %w", telegramID, err)
	}

	_ = a.Store.WriteAuditLog(ctx, models.AuditLog{
		Action:     auditActionDeny,
		TelegramID: &telegramID,
		Actor:      actor,
	})
	return denied, nil
}
