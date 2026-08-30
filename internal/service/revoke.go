package service

import (
	"context"
	"fmt"

	"github.com/barakov-dot/tgproxy-panel-q/internal/models"
)

// Revoke removes an active profile from the host and marks the user revoked.
func (s *Service) Revoke(ctx context.Context, userID int64, actor string) (*models.User, error) {
	u, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("service: revoke user id=%d: %w", userID, err)
	}
	if u.Status != models.StatusActive {
		return u, ErrNotActive
	}

	profileName := ""
	if u.ProfileName != nil {
		profileName = *u.ProfileName
	}

	revoked, err := s.store.UpdateUserStatus(ctx, userID, models.StatusRevoked)
	if err != nil {
		return nil, fmt.Errorf("service: revoke user id=%d: db write: %w", userID, err)
	}

	if _, applyErr := s.applier.RevokeProfile(ctx, u.TelegramID); applyErr != nil {
		s.audit(ctx, auditActionRevokeApplyFail, actor,
			fmt.Sprintf("db updated to revoked but host apply failed for former profile_name=%s: %v", profileName, applyErr),
			userID)
		return nil, fmt.Errorf("%w: %v", ErrRevokeApplyFailed, applyErr)
	}

	s.audit(ctx, auditActionRevoke, actor, fmt.Sprintf("profile_name=%s", profileName), userID)
	return revoked, nil
}

// Deny marks a pending request as denied. No applier call is needed.
func (s *Service) Deny(ctx context.Context, userID int64, actor string) (*models.User, error) {
	u, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("service: deny user id=%d: %w", userID, err)
	}
	if u.Status != models.StatusPending {
		return u, ErrNotPending
	}

	denied, err := s.store.UpdateUserStatus(ctx, userID, models.StatusDenied)
	if err != nil {
		return nil, fmt.Errorf("service: deny user id=%d: db write: %w", userID, err)
	}

	s.audit(ctx, auditActionDeny, actor, "", userID)
	return denied, nil
}
