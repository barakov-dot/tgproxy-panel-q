package service

import (
	"context"

	"github.com/barakov-dot/tgproxy-panel-q/internal/models"
	"github.com/barakov-dot/tgproxy-panel-q/internal/store"
)

// ListUsers returns users matching filter, ordered by sort.
func (s *Service) ListUsers(ctx context.Context, filter store.UserListFilter, sort store.UserListSort) ([]*models.User, error) {
	return s.store.ListUsers(ctx, filter, sort)
}

// GetUser returns a user by primary key.
func (s *Service) GetUser(ctx context.Context, userID int64) (*models.User, error) {
	return s.store.GetUserByID(ctx, userID)
}

// CountPendingUsers returns how many users await admin review.
func (s *Service) CountPendingUsers(ctx context.Context) (int, error) {
	st := models.StatusPending
	users, err := s.store.ListUsers(ctx, store.UserListFilter{Status: &st}, store.UserListSort{
		Column: "requested_at",
		Desc:   true,
	})
	if err != nil {
		return 0, err
	}
	return len(users), nil
}

// SetAutoIssue persists the auto_issue panel toggle.
func (s *Service) SetAutoIssue(ctx context.Context, enabled bool) error {
	v := "false"
	if enabled {
		v = "true"
	}
	return s.store.SetSetting(ctx, models.SettingAutoIssue, v)
}
