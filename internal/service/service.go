// Package service orchestrates issue/revoke/approve/deny/request flows.
// It is the only layer that sequences store writes with applier calls.
package service

import (
	"context"

	"github.com/barakov-dot/tgproxy-panel-q/internal/applier"
	"github.com/barakov-dot/tgproxy-panel-q/internal/config"
	"github.com/barakov-dot/tgproxy-panel-q/internal/models"
	"github.com/barakov-dot/tgproxy-panel-q/internal/secretgen"
	"github.com/barakov-dot/tgproxy-panel-q/internal/store"
)

// Store is the subset of *store.Store this package needs.
type Store interface {
	GetUserByID(ctx context.Context, id int64) (*models.User, error)
	GetUserByTelegramID(ctx context.Context, telegramID int64) (*models.User, error)
	CreateUser(ctx context.Context, telegramID int64, username, firstName, lastName *string) (*models.User, error)
	SetUserProfile(ctx context.Context, id int64, profileName, secret string, setActive bool) (*models.User, error)
	UpdateUserStatus(ctx context.Context, id int64, status models.UserStatus) (*models.User, error)
	ClearUserProfile(ctx context.Context, id int64) (*models.User, error)
	ListUsers(ctx context.Context, filter store.UserListFilter, sort store.UserListSort) ([]*models.User, error)
	GetSetting(ctx context.Context, key string) (string, bool, error)
	SetSetting(ctx context.Context, key, value string) error
	AppendAuditLog(ctx context.Context, entry models.AuditLog) error
}

// Applier pushes desired profiles.json state to the host.
type Applier interface {
	IssueProfile(ctx context.Context, telegramID int64, profileName, secret string) (*applier.Result, error)
	RevokeProfile(ctx context.Context, telegramID int64) (*applier.Result, error)
}

// BotSender delivers proxy links to users in Telegram (implemented by internal/bot).
type BotSender interface {
	SendProxyLink(ctx context.Context, telegramID int64, link string) error
}

// Service holds business logic dependencies.
type Service struct {
	cfg   *config.Config
	store Store
	applier Applier
	bot   BotSender

	GenSecret   func() (string, error)
	ProfileName func(telegramID int64) string
}

// New builds a Service with production defaults.
func New(cfg *config.Config, store Store, ap Applier, bot BotSender) *Service {
	return &Service{
		cfg:         cfg,
		store:       store,
		applier:     ap,
		bot:         bot,
		GenSecret:   secretgen.GenerateSecret,
		ProfileName: secretgen.ProfileName,
	}
}

func (s *Service) genSecret() (string, error) {
	if s.GenSecret != nil {
		return s.GenSecret()
	}
	return secretgen.GenerateSecret()
}

func (s *Service) profileName(telegramID int64) string {
	if s.ProfileName != nil {
		return s.ProfileName(telegramID)
	}
	return secretgen.ProfileName(telegramID)
}

func (s *Service) audit(ctx context.Context, action, actor, detail string, userID int64) {
	id := userID
	_ = s.store.AppendAuditLog(ctx, models.AuditLog{
		Action: action,
		Actor:  actor,
		UserID: &id,
		Detail: detail,
	})
}

// AutoIssueEnabled reads the auto_issue setting, falling back to config when unset.
func (s *Service) AutoIssueEnabled(ctx context.Context) (bool, error) {
	v, ok, err := s.store.GetSetting(ctx, models.SettingAutoIssue)
	if err != nil {
		return false, err
	}
	if !ok {
		return s.cfg.AutoIssue, nil
	}
	return models.AutoIssueEnabled(v), nil
}
