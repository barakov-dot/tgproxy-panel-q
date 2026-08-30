package service

import (
	"context"
	"fmt"

	"github.com/barakov-dot/tgproxy-panel-q/internal/models"
)

// Issue generates a profile, writes the DB row active, then applies on the host.
// If the user is already active with a profile, the existing row is returned
// without re-issuing (idempotent).
func (s *Service) Issue(ctx context.Context, user *models.User, actor string) (*models.User, error) {
	if user.IsActive() && user.HasProfile() {
		return user, nil
	}

	prevStatus := user.Status

	secret, err := s.genSecret()
	if err != nil {
		return nil, fmt.Errorf("service: issue user id=%d: generate secret: %w", user.ID, err)
	}
	profileName := s.profileName(user.TelegramID)

	issued, err := s.store.SetUserProfile(ctx, user.ID, profileName, secret, true)
	if err != nil {
		return nil, fmt.Errorf("service: issue user id=%d: db write: %w", user.ID, err)
	}

	if _, applyErr := s.applier.IssueProfile(ctx, user.TelegramID, profileName, secret); applyErr != nil {
		if rbErr := s.rollbackIssue(ctx, user.ID, prevStatus); rbErr != nil {
			detail := fmt.Sprintf("apply failed for profile_name=%s: %v; DB ROLLBACK ALSO FAILED: %v",
				profileName, applyErr, rbErr)
			s.audit(ctx, auditActionIssueRolledBack, actor, detail, user.ID)
			return nil, fmt.Errorf("%w: %v (and DB rollback also failed: %v — DB may now incorrectly show this user as active, contact an administrator)",
				ErrIssueFailed, applyErr, rbErr)
		}
		s.audit(ctx, auditActionIssueRolledBack, actor,
			fmt.Sprintf("apply failed for profile_name=%s: %v", profileName, applyErr), user.ID)
		return nil, fmt.Errorf("%w: %v", ErrIssueFailed, applyErr)
	}

	s.audit(ctx, auditActionIssue, actor, fmt.Sprintf("profile_name=%s", profileName), user.ID)
	return issued, nil
}

// Approve issues a profile for a pending user (panel or bot admin action).
func (s *Service) Approve(ctx context.Context, userID int64, actor string) (*models.User, error) {
	u, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("service: approve user id=%d: %w", userID, err)
	}
	if u.Status == models.StatusActive {
		return u, ErrAlreadyActive
	}
	if u.Status == models.StatusPending || u.Status == models.StatusRevoked || u.Status == models.StatusDenied {
		return s.Issue(ctx, u, actor)
	}
	return u, ErrNotPending
}

func (s *Service) rollbackIssue(ctx context.Context, userID int64, prevStatus models.UserStatus) error {
	if _, err := s.store.ClearUserProfile(ctx, userID); err != nil {
		return err
	}
	_, err := s.store.UpdateUserStatus(ctx, userID, prevStatus)
	return err
}
