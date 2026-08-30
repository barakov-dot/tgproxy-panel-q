package httpserver

import (
	"context"

	"github.com/barakov-dot/tgproxy-panel/internal/applier"
	"github.com/barakov-dot/tgproxy-panel/internal/models"
)

// userStore is the subset of *store.Store this package needs. Defining it
// here (rather than depending on the concrete *store.Store everywhere) lets
// handler and orchestration tests run against small in-memory fakes without
// a real SQLite file. *store.Store satisfies this interface as-is.
type userStore interface {
	GetUserByID(ctx context.Context, id int64) (*models.User, error)
	GetUserByTelegramID(ctx context.Context, telegramID int64) (*models.User, error)
	ListUsers(ctx context.Context) ([]*models.User, error)
	IssueUser(ctx context.Context, telegramID int64, profileName, secret string) (*models.User, error)
	RevokeUser(ctx context.Context, telegramID int64) (*models.User, error)
	DenyUser(ctx context.Context, telegramID int64) (*models.User, error)
	GetSetting(ctx context.Context, key string) (string, bool, error)
	SetSetting(ctx context.Context, key, value string) error
	WriteAuditLog(ctx context.Context, entry models.AuditLog) error
}

// profileApplier is the subset of *applier.Applier this package needs.
type profileApplier interface {
	IssueProfile(ctx context.Context, telegramID int64, profileName, secret string) (*applier.Result, error)
	RevokeProfile(ctx context.Context, telegramID int64) (*applier.Result, error)
}
