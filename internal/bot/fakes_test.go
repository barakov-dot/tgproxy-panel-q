package bot

import (
	"context"
	"fmt"
	"time"

	"github.com/barakov-dot/tgproxy-panel-q/internal/applier"
	"github.com/barakov-dot/tgproxy-panel-q/internal/config"
	"github.com/barakov-dot/tgproxy-panel-q/internal/models"
	"github.com/barakov-dot/tgproxy-panel-q/internal/service"
	"github.com/barakov-dot/tgproxy-panel-q/internal/store"
)

type fakeStore struct {
	users    map[int64]*models.User
	settings map[string]string
	nextID   int64

	createErr     error
	setProfileErr error
	updateErr     error
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
	now := time.Now()
	u := &models.User{
		ID: f.nextID, TelegramID: telegramID,
		Username: username, FirstName: firstName, LastName: lastName,
		Status: models.StatusPending, RequestedAt: &now,
	}
	f.users[telegramID] = u
	cp := *u
	return &cp, nil
}

func (f *fakeStore) SetUserProfile(_ context.Context, id int64, profileName, secret string, setActive bool) (*models.User, error) {
	if f.setProfileErr != nil {
		return nil, f.setProfileErr
	}
	var u *models.User
	for _, candidate := range f.users {
		if candidate.ID == id {
			u = candidate
			break
		}
	}
	if u == nil {
		return nil, store.ErrNotFound
	}
	u.ProfileName = &profileName
	u.Secret = &secret
	if setActive {
		now := time.Now()
		u.Status = models.StatusActive
		u.IssuedAt = &now
	}
	cp := *u
	return &cp, nil
}

func (f *fakeStore) UpdateUserStatus(_ context.Context, id int64, status models.UserStatus) (*models.User, error) {
	if f.updateErr != nil {
		return nil, f.updateErr
	}
	var u *models.User
	for _, candidate := range f.users {
		if candidate.ID == id {
			u = candidate
			break
		}
	}
	if u == nil {
		return nil, store.ErrNotFound
	}
	u.Status = status
	cp := *u
	return &cp, nil
}

func (f *fakeStore) ClearUserProfile(_ context.Context, id int64) (*models.User, error) {
	var u *models.User
	for _, candidate := range f.users {
		if candidate.ID == id {
			u = candidate
			break
		}
	}
	if u == nil {
		return nil, store.ErrNotFound
	}
	u.ProfileName = nil
	u.Secret = nil
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

func (f *fakeStore) AppendAuditLog(_ context.Context, entry models.AuditLog) error {
	f.auditLogs = append(f.auditLogs, entry)
	return nil
}

func (f *fakeStore) DeleteUser(_ context.Context, id int64) error {
	for tgID, u := range f.users {
		if u.ID == id {
			delete(f.users, tgID)
			return nil
		}
	}
	return store.ErrNotFound
}

func (f *fakeStore) ListUsers(_ context.Context, _ store.UserListFilter, _ store.UserListSort) ([]*models.User, error) {
	out := make([]*models.User, 0, len(f.users))
	for _, u := range f.users {
		cp := *u
		out = append(out, &cp)
	}
	return out, nil
}

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

func testService(st *fakeStore, ap *fakeApplier, defaultAutoIssue bool) *service.Service {
	cfg := &config.Config{
		TproxyHostname: "proxy.example.com",
		AutoIssue:      defaultAutoIssue,
	}
	svc := service.New(cfg, st, ap, nil)
	svc.GenSecret = func() (string, error) { return "deadbeefdeadbeefdeadbeefdeadbeef", nil }
	svc.ProfileName = func(id int64) string { return fmt.Sprintf("user_%d", id) }
	return svc
}
