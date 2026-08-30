package service

import (
	"context"
	"errors"
	"testing"

	"github.com/barakov-dot/tgproxy-panel/internal/models"
	"github.com/barakov-dot/tgproxy-panel/internal/store"
)

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
	ap := &fakeApplier{IssueErr: errors.New("boom")}
	a := testActions(st, ap, true)

	_, err := a.RequestProxy(context.Background(), 105, nil, nil, nil)
	if !errors.Is(err, ErrIssueFailed) {
		t.Fatalf("error = %v, want ErrIssueFailed", err)
	}
	// Approve's rollback-on-apply-failure kicks in here (unlike the old
	// pre-consolidation bot behavior, which left the row active) — the DB
	// must not claim active for a profile the host doesn't have.
	u, lookupErr := st.GetUserByTelegramID(context.Background(), 105)
	if lookupErr != nil {
		t.Fatalf("lookup: %v", lookupErr)
	}
	if u.Status == models.StatusActive {
		t.Fatalf("user after failed apply = %+v, want status rolled back off active", u)
	}
	found := false
	for _, e := range st.audit {
		if e.Action == auditActionIssueRolledBack {
			found = true
		}
	}
	if !found {
		t.Errorf("audit logs = %+v, want an issue_failed_rollback entry", st.audit)
	}
}

func TestRequestProxy_ReopensRevokedUserAsPending(t *testing.T) {
	st := newFakeStore()
	ap := &fakeApplier{}
	a := testActions(st, ap, false)
	ctx := context.Background()

	if _, err := st.CreateUser(ctx, 106, nil, nil, nil); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if _, err := st.IssueUser(ctx, 106, "user_106", "deadbeefdeadbeefdeadbeefdeadbeef"); err != nil {
		t.Fatalf("IssueUser: %v", err)
	}
	if _, err := st.RevokeUser(ctx, 106); err != nil {
		t.Fatalf("RevokeUser: %v", err)
	}

	res, err := a.RequestProxy(ctx, 106, nil, nil, nil)
	if err != nil {
		t.Fatalf("RequestProxy() error = %v", err)
	}
	if res.Outcome != OutcomePendingCreated {
		t.Fatalf("Outcome = %v, want OutcomePendingCreated", res.Outcome)
	}
	if res.User.Status != models.StatusPending {
		t.Fatalf("Status = %v, want pending (revoked user should be reopened, not left revoked)", res.User.Status)
	}
}

func TestApprove_FromPendingRequest(t *testing.T) {
	st := newFakeStore()
	ap := &fakeApplier{}
	a := testActions(st, ap, false)
	ctx := context.Background()

	if _, err := a.RequestProxy(ctx, 107, nil, nil, nil); err != nil {
		t.Fatalf("RequestProxy() error = %v", err)
	}

	u, err := a.Approve(ctx, 107, ActorAdmin(999))
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

func TestDeny_FromPendingRequest(t *testing.T) {
	st := newFakeStore()
	ap := &fakeApplier{}
	a := testActions(st, ap, false)
	ctx := context.Background()

	if _, err := a.RequestProxy(ctx, 108, nil, nil, nil); err != nil {
		t.Fatalf("RequestProxy() error = %v", err)
	}

	u, err := a.Deny(ctx, 108, ActorAdmin(999))
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
	for _, e := range st.audit {
		if e.Action == auditActionDeny && e.TelegramID != nil && *e.TelegramID == 108 {
			found = true
		}
	}
	if !found {
		t.Errorf("audit logs = %+v, want a deny entry for 108", st.audit)
	}
}

func TestDeny_UnknownUser(t *testing.T) {
	st := newFakeStore()
	ap := &fakeApplier{}
	a := testActions(st, ap, false)

	if _, err := a.Deny(context.Background(), 999999, ActorAdmin(1)); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
}
