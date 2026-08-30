package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/barakov-dot/tgproxy-panel/internal/models"
)

var (
	// ErrAlreadyActive is returned by Approve when the target user already
	// has an active profile — nothing to do.
	ErrAlreadyActive = errors.New("service: user already has an active profile")

	// ErrNotPending is returned by Deny when the target user isn't
	// currently pending.
	ErrNotPending = errors.New("service: user is not pending")

	// ErrNotActive is returned by Revoke when the target user has no
	// active profile to revoke.
	ErrNotActive = errors.New("service: user has no active profile to revoke")

	// ErrIssueFailed wraps an applier failure during Approve, after the DB
	// rollback (best-effort) has been attempted. The caller-facing message
	// should make clear the host was not updated.
	ErrIssueFailed = errors.New("service: issuing the profile on the host failed")

	// ErrRevokeApplyFailed wraps an applier failure during Revoke. Unlike
	// ErrIssueFailed there is no DB rollback here — see Revoke's doc
	// comment for why leaving the DB row revoked is the safer of the two
	// bad options when the host apply itself fails.
	ErrRevokeApplyFailed = errors.New("service: applying the revoke on the host failed")
)

const (
	auditActionIssue           = "issue"
	auditActionIssueRolledBack = "issue_failed_rollback"
	auditActionRevoke          = "revoke"
	auditActionRevokeApplyFail = "revoke_apply_failed"
	auditActionDeny            = "deny"
)

// Approve issues a fresh profile for telegramID: generates a secret and
// profile name, writes the DB row active (Store.IssueUser), then pushes the
// resulting desired state to the host (Applier.IssueProfile). It is used
// both for approving a pending request and for re-issuing access to a
// previously revoked/denied user — Store.IssueUser doesn't care about the
// prior status, and Applier.IssueProfile only checks that the row is active
// with the profile name/secret just written. RequestProxy's auto-issue path
// calls this too, so all three issuing paths (panel approve, bot approve,
// bot auto-issue) share one implementation.
//
// Ordering matches applier.IssueProfile's documented contract: DB write
// first, then apply. If the apply fails, the DB would be left claiming an
// active profile the host was never actually given — that must never
// happen, so on apply failure this method attempts to roll the DB row back
// (Store.RevokeUser, which also clears profile_name/secret) before
// returning ErrIssueFailed. If even that rollback fails, both failures are
// logged to the audit trail (never silently) and the returned error says so
// explicitly — the caller must surface a "partially failed, contact admin"
// message, not a generic error.
func (a *Actions) Approve(ctx context.Context, telegramID int64, actor string) (*models.User, error) {
	u, err := a.Store.GetUserByTelegramID(ctx, telegramID)
	if err != nil {
		return nil, fmt.Errorf("service: approve telegram_id=%d: %w", telegramID, err)
	}
	if u.Status == models.StatusActive {
		return u, ErrAlreadyActive
	}

	secret, err := a.genSecret()
	if err != nil {
		return nil, fmt.Errorf("service: approve telegram_id=%d: generate secret: %w", telegramID, err)
	}
	profileName := a.profileName(telegramID)

	issued, err := a.Store.IssueUser(ctx, telegramID, profileName, secret)
	if err != nil {
		return nil, fmt.Errorf("service: approve telegram_id=%d: db write: %w", telegramID, err)
	}

	if _, applyErr := a.Applier.IssueProfile(ctx, telegramID, profileName, secret); applyErr != nil {
		_, rollbackErr := a.Store.RevokeUser(ctx, telegramID)

		detail := fmt.Sprintf("apply failed for profile_name=%s: %v", profileName, applyErr)
		if rollbackErr != nil {
			detail += fmt.Sprintf("; DB ROLLBACK ALSO FAILED: %v (db may still say active for a profile the host does not have)", rollbackErr)
		}
		a.audit(ctx, auditActionIssueRolledBack, telegramID, actor, detail)

		if rollbackErr != nil {
			return nil, fmt.Errorf("%w: %v (and DB rollback also failed: %v — DB may now incorrectly show this user as active, contact an administrator)",
				ErrIssueFailed, applyErr, rollbackErr)
		}
		return nil, fmt.Errorf("%w: %v", ErrIssueFailed, applyErr)
	}

	a.audit(ctx, auditActionIssue, telegramID, actor, fmt.Sprintf("profile_name=%s", profileName))
	return issued, nil
}

// Revoke removes telegramID's active profile: writes the DB row revoked
// (Store.RevokeUser, which also clears profile_name/secret), then pushes
// the resulting desired state to the host (Applier.RevokeProfile).
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
// rollback attempt, just a loud audit entry and a caller-facing error
// saying the host was not confirmed updated.
func (a *Actions) Revoke(ctx context.Context, telegramID int64, actor string) (*models.User, error) {
	u, err := a.Store.GetUserByTelegramID(ctx, telegramID)
	if err != nil {
		return nil, fmt.Errorf("service: revoke telegram_id=%d: %w", telegramID, err)
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
		return nil, fmt.Errorf("service: revoke telegram_id=%d: db write: %w", telegramID, err)
	}

	if _, applyErr := a.Applier.RevokeProfile(ctx, telegramID); applyErr != nil {
		a.audit(ctx, auditActionRevokeApplyFail, telegramID, actor,
			fmt.Sprintf("db updated to revoked but host apply failed for former profile_name=%s: %v", profileName, applyErr))
		return nil, fmt.Errorf("%w: %v", ErrRevokeApplyFailed, applyErr)
	}

	a.audit(ctx, auditActionRevoke, telegramID, actor, fmt.Sprintf("profile_name=%s", profileName))
	return revoked, nil
}

// Deny marks a pending request denied. No applier call: a pending request
// was never applied to the host.
func (a *Actions) Deny(ctx context.Context, telegramID int64, actor string) (*models.User, error) {
	u, err := a.Store.GetUserByTelegramID(ctx, telegramID)
	if err != nil {
		return nil, fmt.Errorf("service: deny telegram_id=%d: %w", telegramID, err)
	}
	if u.Status != models.StatusPending {
		return u, ErrNotPending
	}

	denied, err := a.Store.DenyUser(ctx, telegramID)
	if err != nil {
		return nil, fmt.Errorf("service: deny telegram_id=%d: db write: %w", telegramID, err)
	}

	a.audit(ctx, auditActionDeny, telegramID, actor, "")
	return denied, nil
}
