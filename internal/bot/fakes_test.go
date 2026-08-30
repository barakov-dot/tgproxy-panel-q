package bot

import (
	"context"
	"fmt"

	"github.com/barakov-dot/tgproxy-panel/internal/applier"
	"github.com/barakov-dot/tgproxy-panel/internal/models"
	"github.com/barakov-dot/tgproxy-panel/internal/service"
	"github.com/barakov-dot/tgproxy-panel/internal/store"
)

// fakeStore is a minimal in-memory stand-in for *store.Store, scoped to
// service.Store (what *service.Actions needs). Kept in this package rather
// than shared from internal/service's own test fakes since those are
// unexported test-only types.
type fakeStore struct {
	users    map[int64]*models.User
	settings map[string]string
	nextID   int64

	createErr     error
	issueErr      error
	denyErr       error
	getSettingErr error

	auditLogs []models.AuditLog
}

func newFakeStore() *fakeStore {
	return &fakeStore{users: map[int64]*models.User{}, settings: map[string]string{}}
}

func (f *fakeStore) GetUserByID(_ context.Context, id int64) (*models.User, error) {
	for _, u := range f.users {
		if u.ID == id {
			cp := *u
			return &cp, nil
		}
	}
	return nil, store.ErrNotFound
}

func (f *fakeStore) GetUserByTelegramID(_ context.Context, telegramID int64) (*models.User, error) {
	u, ok := f.users[telegramID]
	if !ok {
		return nil, store.ErrNotFound
	}
	cp := *u
	return &cp, nil
}

func (f *fakeStore) CreateUser(_ context.Context, telegramID int64, username, firstName, lastName *string) (*models.User, error) {
	if f.createErr != nil {
		return nil, f.createErr
	}
	f.nextID++
	u := &models.User{
		ID: f.nextID, TelegramID: telegramID,
		Username: username, FirstName: firstName, LastName: lastName,
		Status: models.StatusPending,
	}
	f.users[telegramID] = u
	cp := *u
	return &cp, nil
}

func (f *fakeStore) ListUsers(_ context.Context) ([]*models.User, error) {
	out := make([]*models.User, 0, len(f.users))
	for _, u := range f.users {
		cp := *u
		out = append(out, &cp)
	}
	return out, nil
}

func (f *fakeStore) IssueUser(_ context.Context, telegramID int64, profileName, secret string) (*models.User, error) {
	if f.issueErr != nil {
		return nil, f.issueErr
	}
	u, ok := f.users[telegramID]
	if !ok {
		return nil, store.ErrNotFound
	}
	u.Status = models.StatusActive
	u.ProfileName = &profileName
	u.Secret = &secret
	cp := *u
	return &cp, nil
}

func (f *fakeStore) RevokeUser(_ context.Context, telegramID int64) (*models.User, error) {
	u, ok := f.users[telegramID]
	if !ok {
		return nil, store.ErrNotFound
	}
	u.Status = models.StatusRevoked
	u.ProfileName = nil
	u.Secret = nil
	cp := *u
	return &cp, nil
}

func (f *fakeStore) DenyUser(_ context.Context, telegramID int64) (*models.User, error) {
	if f.denyErr != nil {
		return nil, f.denyErr
	}
	u, ok := f.users[telegramID]
	if !ok {
		return nil, store.ErrNotFound
	}
	u.Status = models.StatusDenied
	cp := *u
	return &cp, nil
}

func (f *fakeStore) SetPending(_ context.Context, telegramID int64) (*models.User, error) {
	u, ok := f.users[telegramID]
	if !ok {
		return nil, store.ErrNotFound
	}
	u.Status = models.StatusPending
	cp := *u
	return &cp, nil
}

func (f *fakeStore) GetSetting(_ context.Context, key string) (string, bool, error) {
	if f.getSettingErr != nil {
		return "", false, f.getSettingErr
	}
	v, ok := f.settings[key]
	return v, ok, nil
}

func (f *fakeStore) SetSetting(_ context.Context, key, value string) error {
	f.settings[key] = value
	return nil
}

func (f *fakeStore) WriteAuditLog(_ context.Context, entry models.AuditLog) error {
	f.auditLogs = append(f.auditLogs, entry)
	return nil
}

// fakeApplier is a minimal stand-in for *applier.Applier, scoped to
// service.Applier.
type fakeApplier struct {
	err   error
	calls []applierCall
}

type applierCall struct {
	TelegramID  int64
	ProfileName string
	Secret      string
}

func (f *fakeApplier) IssueProfile(_ context.Context, telegramID int64, profileName, secret string) (*applier.Result, error) {
	f.calls = append(f.calls, applierCall{telegramID, profileName, secret})
	if f.err != nil {
		return nil, f.err
	}
	return &applier.Result{ProfileCount: 1}, nil
}

func (f *fakeApplier) RevokeProfile(_ context.Context, telegramID int64) (*applier.Result, error) {
	f.calls = append(f.calls, applierCall{TelegramID: telegramID})
	if f.err != nil {
		return nil, f.err
	}
	return &applier.Result{ProfileCount: 0}, nil
}

func testActions(st *fakeStore, ap *fakeApplier, defaultAutoIssue bool) *service.Actions {
	return &service.Actions{
		Store:            st,
		Applier:          ap,
		DefaultAutoIssue: defaultAutoIssue,
		GenSecret:        func() (string, error) { return "deadbeefdeadbeefdeadbeefdeadbeef", nil },
		ProfileName:      func(id int64) string { return fmt.Sprintf("user_%d", id) },
	}
}
