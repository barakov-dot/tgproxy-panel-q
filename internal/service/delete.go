package service

import (
	"context"
	"fmt"

	"github.com/barakov-dot/tgproxy-panel-q/internal/models"
)

// Delete removes a revoked or denied user row from the panel database.
// Active and pending users must be revoked or denied first.
func (s *Service) Delete(ctx context.Context, userID int64, actor string) error {
	u, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("service: delete user id=%d: %w", userID, err)
	}

	switch u.Status {
	case models.StatusRevoked, models.StatusDenied:
	default:
		return ErrNotDeletable
	}

	s.audit(ctx, auditActionDelete, actor,
		fmt.Sprintf("telegram_id=%d status=%s", u.TelegramID, u.Status), userID)

	if err := s.store.DeleteUser(ctx, userID); err != nil {
		return fmt.Errorf("service: delete user id=%d: db: %w", userID, err)
	}
	return nil
}
