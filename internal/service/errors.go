package service

import "errors"

var (
	ErrAlreadyActive     = errors.New("service: user already has an active profile")
	ErrNotPending        = errors.New("service: user is not pending")
	ErrNotActive         = errors.New("service: user has no active profile to revoke")
	ErrIssueFailed       = errors.New("service: issuing the profile on the host failed")
	ErrRevokeApplyFailed = errors.New("service: applying the revoke on the host failed")
	ErrNoBotSender       = errors.New("service: bot sender not configured")
	ErrNoProxyLink       = errors.New("service: user has no proxy link")
)

const (
	auditActionIssue           = "issue"
	auditActionIssueRolledBack = "issue_failed_rollback"
	auditActionRevoke          = "revoke"
	auditActionRevokeApplyFail = "revoke_apply_failed"
	auditActionDeny            = "deny"
	auditActionResend          = "resend"
)

// ActorAutoIssue identifies audit entries for automatic issuance.
const ActorAutoIssue = "auto_issue"
