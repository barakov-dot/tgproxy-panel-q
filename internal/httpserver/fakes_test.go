package httpserver

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/barakov-dot/tgproxy-panel/internal/applier"
	"github.com/barakov-dot/tgproxy-panel/internal/models"
	"github.com/barakov-dot/tgproxy-panel/internal/store"
)

// fakeStore is an in-memory userStore for tests, avoiding a real SQLite
// file for anything that doesn't specifically want one.
type fakeStore struct {
	mu       sync.Mutex
	nextID   int64
	users    map[int64]*models.User // keyed by telegram_id
	settings map[string]string
	audit    []models.AuditLog
}

func newFakeStore() *fakeStore {
	return &fakeStore{users: map[int64]*models.User{}, settings: map[string]string{}}
}

func (f *fakeStore) addUser(u *models.User) *models.User {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	u.ID = f.nextID
	cp := *u
	f.users[u.TelegramID] = &cp
	return &cp
}

func (f *fakeStore) GetUserByID(ctx context.Context, id int64) (*models.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, u := range f.users {
		if u.ID == id {
			cp := *u
			return &cp, nil
		}
	}
	return nil, store.ErrNotFound
}

func (f *fakeStore) GetUserByTelegramID(ctx context.Context, telegramID int64) (*models.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, ok := f.users[telegramID]
	if !ok {
		return nil, store.ErrNotFound
	}
	cp := *u
	return &cp, nil
}

func (f *fakeStore) ListUsers(ctx context.Context) ([]*models.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*models.User, 0, len(f.users))
	for _, u := range f.users {
		cp := *u
		out = append(out, &cp)
	}
	return out, nil
}

func (f *fakeStore) IssueUser(ctx context.Context, telegramID int64, profileName, secret string) (*models.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, ok := f.users[telegramID]
	if !ok {
		return nil, store.ErrNotFound
	}
	now := time.Now()
	u.ProfileName = &profileName
	u.Secret = &secret
	u.Status = models.StatusActive
	u.IssuedAt = &now
	cp := *u
	return &cp, nil
}

func (f *fakeStore) RevokeUser(ctx context.Context, telegramID int64) (*models.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, ok := f.users[telegramID]
	if !ok {
		return nil, store.ErrNotFound
	}
	now := time.Now()
	u.ProfileName = nil
	u.Secret = nil
	u.Status = models.StatusRevoked
	u.RevokedAt = &now
	cp := *u
	return &cp, nil
}

func (f *fakeStore) DenyUser(ctx context.Context, telegramID int64) (*models.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, ok := f.users[telegramID]
	if !ok {
		return nil, store.ErrNotFound
	}
	u.Status = models.StatusDenied
	cp := *u
	return &cp, nil
}

func (f *fakeStore) GetSetting(ctx context.Context, key string) (string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.settings[key]
	return v, ok, nil
}

func (f *fakeStore) SetSetting(ctx context.Context, key, value string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.settings[key] = value
	return nil
}

func (f *fakeStore) WriteAuditLog(ctx context.Context, entry models.AuditLog) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.audit = append(f.audit, entry)
	return nil
}

// fakeApplier is an in-memory profileApplier for tests. Set IssueErr/RevokeErr
// to force a failure path.
type fakeApplier struct {
	mu          sync.Mutex
	IssueErr    error
	RevokeErr   error
	IssueCalls  int
	RevokeCalls int
}

func (f *fakeApplier) IssueProfile(ctx context.Context, telegramID int64, profileName, secret string) (*applier.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.IssueCalls++
	if f.IssueErr != nil {
		return nil, f.IssueErr
	}
	return &applier.Result{ProfileCount: 1}, nil
}

func (f *fakeApplier) RevokeProfile(ctx context.Context, telegramID int64) (*applier.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.RevokeCalls++
	if f.RevokeErr != nil {
		return nil, f.RevokeErr
	}
	return &applier.Result{ProfileCount: 0}, nil
}

var errForced = fmt.Errorf("forced test error")
