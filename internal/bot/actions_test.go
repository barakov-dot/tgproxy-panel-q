package bot

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/barakov-dot/tgproxy-panel/internal/applier"
	"github.com/barakov-dot/tgproxy-panel/internal/models"
	"github.com/barakov-dot/tgproxy-panel/internal/store"
)

// fakeStore is a minimal in-memory stand-in for *store.Store, scoped to the
// bot.Store interface only.
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

func (f *fakeStore) GetSetting(_ context.Context, key string) (string, bool, error) {
	if f.getSettingErr != nil {
		return "", false, f.getSettingErr
	}
	v, ok := f.settings[key]
	return v, ok, nil
}

func (f *fakeStore) WriteAuditLog(_ context.Context, entry models.AuditLog) error {
	f.auditLogs = append(f.auditLogs, entry)
	return nil
}

// fakeApplier is a minimal stand-in for *applier.Applier, scoped to the
// bot.Applier interface only.
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

func testActions(st *fakeStore, ap *fakeApplier, defaultAutoIssue bool) *Actions {
	return &Actions{
		Store:            st,
		Applier:          ap,
		DefaultAutoIssue: defaultAutoIssue,
		GenerateSecret:   func() (string, error) { return "deadbeefdeadbeefdeadbeefdeadbeef", nil },
		ProfileName:      func(id int64) string { return fmt.Sprintf("user_%d", id) },
	}
}

func TestRequestProxy_NewUserAutoIssueOn(t *testing.T) {
	st := newFakeStore()
	ap := &fakeApplier{}
	a := testActions(st, ap, true)

	res, err := a.RequestProxy(context.Background(), 100, nil, nil, nil)
	if err != nil {
		t.Fatalf("RequestProxy() error = %v", err)
	}
	if res.Outcome != OutcomeIssued {
		t.Fatalf("Outcome = %v, want OutcomeIssued", res.Outcome)
	}
	if res.User.Status != models.StatusActive {
		t.Errorf("Status = %v, want active", res.User.Status)
	}
	if len(ap.calls) != 1 || ap.calls[0].TelegramID != 100 {
		t.Errorf("applier calls = %+v", ap.calls)
	}
}

func TestRequestProxy_NewUserAutoIssueOff(t *testing.T) {
	st := newFakeStore()
	ap := &fakeApplier{}
	a := testActions(st, ap, false)

	res, err := a.RequestProxy(context.Background(), 101, nil, nil, nil)
	if err != nil {
		t.Fatalf("RequestProxy() error = %v", err)
	}
	if res.Outcome != OutcomePendingCreated {
		t.Fatalf("Outcome = %v, want OutcomePendingCreated", res.Outcome)
	}
	if res.User.Status != models.StatusPending {
		t.Errorf("Status = %v, want pending", res.User.Status)
	}
	if len(ap.calls) != 0 {
		t.Errorf("applier calls = %+v, want none", ap.calls)
	}
}

func TestRequestProxy_UsesSettingOverDefault(t *testing.T) {
	st := newFakeStore()
	st.settings[models.SettingAutoIssue] = "true"
	ap := &fakeApplier{}
	a := testActions(st, ap, false) // default off, setting says on

	res, err := a.RequestProxy(context.Background(), 102, nil, nil, nil)
	if err != nil {
		t.Fatalf("RequestProxy() error = %v", err)
	}
	if res.Outcome != OutcomeIssued {
		t.Fatalf("Outcome = %v, want OutcomeIssued (setting should override default)", res.Outcome)
	}
}

func TestRequestProxy_AlreadyActive(t *testing.T) {
	st := newFakeStore()
	ap := &fakeApplier{}
	a := testActions(st, ap, true)
	ctx := context.Background()

	first, err := a.RequestProxy(ctx, 103, nil, nil, nil)
	if err != nil {
		t.Fatalf("first RequestProxy() error = %v", err)
	}

	second, err := a.RequestProxy(ctx, 103, nil, nil, nil)
	if err != nil {
		t.Fatalf("second RequestProxy() error = %v", err)
	}
	if second.Outcome != OutcomeAlreadyActive {
		t.Fatalf("Outcome = %v, want OutcomeAlreadyActive", second.Outcome)
	}
	if *second.User.Secret != *first.User.Secret {
		t.Errorf("secret changed between calls: %q vs %q", *first.User.Secret, *second.User.Secret)
	}
	if len(ap.calls) != 1 {
		t.Errorf("applier calls = %d, want 1 (no re-issue)", len(ap.calls))
	}
}

func TestRequestProxy_AlreadyPending(t *testing.T) {
	st := newFakeStore()
	ap := &fakeApplier{}
	a := testActions(st, ap, false)
	ctx := context.Background()

	if _, err := a.RequestProxy(ctx, 104, nil, nil, nil); err != nil {
		t.Fatalf("first RequestProxy() error = %v", err)
	}
	res, err := a.RequestProxy(ctx, 104, nil, nil, nil)
	if err != nil {
		t.Fatalf("second RequestProxy() error = %v", err)
	}
	if res.Outcome != OutcomeAlreadyPending {
		t.Fatalf("Outcome = %v, want OutcomeAlreadyPending", res.Outcome)
	}
}

func TestRequestProxy_ApplierFailure(t *testing.T) {
	st := newFakeStore()
	ap := &fakeApplier{err: errors.New("boom")}
	a := testActions(st, ap, true)

	_, err := a.RequestProxy(context.Background(), 105, nil, nil, nil)
	if !errors.Is(err, ErrApplyFailed) {
		t.Fatalf("error = %v, want ErrApplyFailed", err)
	}
	// DB write already happened before the applier call per the ordering
	// contract, so the row is left active even though apply failed.
	u := st.users[105]
	if u == nil || u.Status != models.StatusActive {
		t.Fatalf("user after failed apply = %+v, want status=active", u)
	}
	found := false
	for _, e := range st.auditLogs {
		if e.Action == "issue_apply_failed" {
			found = true
		}
	}
	if !found {
		t.Errorf("audit logs = %+v, want an issue_apply_failed entry", st.auditLogs)
	}
}

func TestApprove(t *testing.T) {
	st := newFakeStore()
	ap := &fakeApplier{}
	a := testActions(st, ap, false)
	ctx := context.Background()

	if _, err := a.RequestProxy(ctx, 106, nil, nil, nil); err != nil {
		t.Fatalf("RequestProxy() error = %v", err)
	}

	u, err := a.Approve(ctx, 106, 999)
	if err != nil {
		t.Fatalf("Approve() error = %v", err)
	}
	if u.Status != models.StatusActive {
		t.Errorf("Status = %v, want active", u.Status)
	}
	if len(ap.calls) != 1 {
		t.Errorf("applier calls = %d, want 1", len(ap.calls))
	}
}

func TestDeny(t *testing.T) {
	st := newFakeStore()
	ap := &fakeApplier{}
	a := testActions(st, ap, false)
	ctx := context.Background()

	if _, err := a.RequestProxy(ctx, 107, nil, nil, nil); err != nil {
		t.Fatalf("RequestProxy() error = %v", err)
	}

	u, err := a.Deny(ctx, 107, 999)
	if err != nil {
		t.Fatalf("Deny() error = %v", err)
	}
	if u.Status != models.StatusDenied {
		t.Errorf("Status = %v, want denied", u.Status)
	}
	if len(ap.calls) != 0 {
		t.Errorf("applier calls = %d, want 0", len(ap.calls))
	}

	found := false
	for _, e := range st.auditLogs {
		if e.Action == "deny" && e.TelegramID != nil && *e.TelegramID == 107 {
			found = true
		}
	}
	if !found {
		t.Errorf("audit logs = %+v, want a deny entry for 107", st.auditLogs)
	}
}

func TestDeny_UnknownUser(t *testing.T) {
	st := newFakeStore()
	ap := &fakeApplier{}
	a := testActions(st, ap, false)

	if _, err := a.Deny(context.Background(), 999999, 1); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
}
