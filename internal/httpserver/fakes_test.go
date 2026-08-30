package httpserver

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/barakov-dot/tgproxy-panel-q/internal/applier"
	"github.com/barakov-dot/tgproxy-panel-q/internal/models"
	"github.com/barakov-dot/tgproxy-panel-q/internal/store"
)

type fakeStore struct {
	mu       sync.Mutex
	nextID   int64
	users    map[int64]*models.User
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

func (f *fakeStore) GetUserByID(_ context.Context, id int64) (*models.User, error) {
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

func (f *fakeStore) GetUserByTelegramID(_ context.Context, telegramID int64) (*models.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, ok := f.users[telegramID]
	if !ok {
		return nil, store.ErrNotFound
	}
	cp := *u
	return &cp, nil
}

func (f *fakeStore) CreateUser(_ context.Context, telegramID int64, username, firstName, lastName *string) (*models.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
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
	f.mu.Lock()
	defer f.mu.Unlock()
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
	f.mu.Lock()
	defer f.mu.Unlock()
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
	now := time.Now()
	u.Status = status
	switch status {
	case models.StatusPending:
		u.RequestedAt = &now
	case models.StatusRevoked:
		u.ProfileName = nil
		u.Secret = nil
		u.RevokedAt = &now
	}
	cp := *u
	return &cp, nil
}

func (f *fakeStore) ClearUserProfile(_ context.Context, id int64) (*models.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
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

func (f *fakeStore) ListUsers(_ context.Context, filter store.UserListFilter, order store.UserListSort) ([]*models.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*models.User, 0, len(f.users))
	for _, u := range f.users {
		cp := *u
		out = append(out, &cp)
	}
	if filter.Status != nil {
		filtered := out[:0]
		for _, u := range out {
			if u.Status == *filter.Status {
				filtered = append(filtered, u)
			}
		}
		out = filtered
	}
	if q := strings.TrimSpace(filter.Query); q != "" {
		lq := strings.ToLower(q)
		filtered := out[:0]
		for _, u := range out {
			if fakeUserMatchesQuery(u, lq) {
				filtered = append(filtered, u)
			}
		}
		out = filtered
	}
	fakeSortUsers(out, order)
	return out, nil
}

func fakeUserMatchesQuery(u *models.User, lowerQuery string) bool {
	fields := []string{
		strconv.FormatInt(u.TelegramID, 10),
		string(u.Status),
		u.DisplayName(),
	}
	if u.Username != nil {
		fields = append(fields, *u.Username)
	}
	for _, field := range fields {
		if strings.Contains(strings.ToLower(field), lowerQuery) {
			return true
		}
	}
	return false
}

func fakeSortUsers(users []*models.User, order store.UserListSort) {
	col := order.Column
	if col == "" {
		col = "requested_at"
	}
	sort.SliceStable(users, func(i, j int) bool {
		a, b := users[i], users[j]
		var less bool
		switch col {
		case "telegram_id":
			less = a.TelegramID < b.TelegramID
		case "username":
			less = strings.ToLower(a.DisplayName()) < strings.ToLower(b.DisplayName())
		case "status":
			less = a.Status < b.Status
		case "issued_at":
			less = fakeTimeLess(a.IssuedAt, b.IssuedAt)
		default:
			less = fakeTimeLess(a.RequestedAt, b.RequestedAt)
		}
		if order.Desc {
			return !less
		}
		return less
	})
}

func fakeTimeLess(a, b *time.Time) bool {
	if a == nil && b == nil {
		return false
	}
	if a == nil {
		return true
	}
	if b == nil {
		return false
	}
	return a.Before(*b)
}

func (f *fakeStore) GetSetting(_ context.Context, key string) (string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.settings[key]
	return v, ok, nil
}

func (f *fakeStore) SetSetting(_ context.Context, key, value string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.settings[key] = value
	return nil
}

func (f *fakeStore) AppendAuditLog(_ context.Context, entry models.AuditLog) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.audit = append(f.audit, entry)
	return nil
}

type fakeApplier struct {
	mu          sync.Mutex
	IssueErr    error
	RevokeErr   error
	IssueCalls  int
	RevokeCalls int
}

func (f *fakeApplier) IssueProfile(_ context.Context, telegramID int64, profileName, secret string) (*applier.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.IssueCalls++
	if f.IssueErr != nil {
		return nil, f.IssueErr
	}
	return &applier.Result{ProfileCount: 1}, nil
}

func (f *fakeApplier) RevokeProfile(_ context.Context, telegramID int64) (*applier.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.RevokeCalls++
	if f.RevokeErr != nil {
		return nil, f.RevokeErr
	}
	return &applier.Result{ProfileCount: 0}, nil
}

var errForced = fmt.Errorf("forced test error")
