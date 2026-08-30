package service

import (
	"context"
	"fmt"

	"github.com/barakov-dot/tgproxy-panel-q/internal/models"
)

// GetProxyLink builds the t.me/webproxy link for an active user.
func (s *Service) GetProxyLink(user *models.User) string {
	return user.ProxyLink(s.cfg.TproxyHostname)
}

// Resend sends the proxy link to the user via Telegram when they are active.
func (s *Service) Resend(ctx context.Context, userID int64, actor string) error {
	u, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("service: resend user id=%d: %w", userID, err)
	}
	if !u.IsActive() {
		return ErrNotActive
	}

	link := s.GetProxyLink(u)
	if link == "" {
		return ErrNoProxyLink
	}
	if s.bot == nil {
		return ErrNoBotSender
	}
	if err := s.bot.SendProxyLink(ctx, u.TelegramID, link); err != nil {
		return fmt.Errorf("service: resend user id=%d: %w", userID, err)
	}

	s.audit(ctx, auditActionResend, actor, "", userID)
	return nil
}
