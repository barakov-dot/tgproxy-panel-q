package service

import (
	"context"
	"errors"
	"testing"

	"github.com/barakov-dot/tgproxy-panel/internal/models"
)

func TestApproveIssuesProfile(t *testing.T) {
	fs := newFakeStore()
	fs.addUser(&models.User{TelegramID: 111, Status: models.StatusPending})
	fa := &fakeApplier{}
	a := testActions(fs, fa, false)

	u, err := a.Approve(context.Background(), 111, "test-admin")
	if err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if u.Status != models.StatusActive {
		t.Errorf("status = %s, want active", u.Status)
	}
	if u.Secret == nil || *u.Secret == "" {
		t.Error("expected a secret to be set")
	}
	if fa.IssueCalls != 1 {
		t.Errorf("IssueCalls = %d, want 1", fa.IssueCalls)
	}
	if len(fs.audit) != 1 || fs.audit[0].Action != auditActionIssue {
		t.Errorf("expected one 'issue' audit entry, got %+v", fs.audit)
	}
}

func TestApproveAlreadyActive(t *testing.T) {
	fs := newFakeStore()
	secret := "abc"
	fs.addUser(&models.User{TelegramID: 111, Status: models.StatusActive, Secret: &secret})
	fa := &fakeApplier{}
	a := testActions(fs, fa, false)

	_, err := a.Approve(context.Background(), 111, "test-admin")
	if !errors.Is(err, ErrAlreadyActive) {
		t.Fatalf("err = %v, want ErrAlreadyActive", err)
	}
	if fa.IssueCalls != 0 {
		t.Errorf("IssueCalls = %d, want 0 (should not touch the applier)", fa.IssueCalls)
	}
}

// TestApproveRollsBackOnApplierFailure is the key regression test for
// CLAUDE.md's "never leave the DB active if the host wasn't updated" rule.
func TestApproveRollsBackOnApplierFailure(t *testing.T) {
	fs := newFakeStore()
	fs.addUser(&models.User{TelegramID: 111, Status: models.StatusPending})
	fa := &fakeApplier{IssueErr: errForced}
	a := testActions(fs, fa, false)

	_, err := a.Approve(context.Background(), 111, "test-admin")
	if !errors.Is(err, ErrIssueFailed) {
		t.Fatalf("err = %v, want ErrIssueFailed", err)
	}

	got, lookupErr := fs.GetUserByTelegramID(context.Background(), 111)
	if lookupErr != nil {
		t.Fatalf("lookup: %v", lookupErr)
	}
	if got.Status == models.StatusActive {
		t.Fatal("DB row was left active after applier failure — must be rolled back")
	}
	if got.Secret != nil {
		t.Error("secret should have been cleared by the rollback")
	}

	found := false
	for _, e := range fs.audit {
		if e.Action == auditActionIssueRolledBack {
			found = true
		}
	}
	if !found {
		t.Error("expected an issue_failed_rollback audit entry")
	}
}

func TestApproveReissuesRevokedUser(t *testing.T) {
	fs := newFakeStore()
	fs.addUser(&models.User{TelegramID: 111, Status: models.StatusRevoked})
	fa := &fakeApplier{}
	a := testActions(fs, fa, false)

	u, err := a.Approve(context.Background(), 111, "test-admin")
	if err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if u.Status != models.StatusActive {
		t.Errorf("status = %s, want active", u.Status)
	}
}

func TestRevokeClearsProfile(t *testing.T) {
	fs := newFakeStore()
	secret := "abc"
	name := "user_111"
	fs.addUser(&models.User{TelegramID: 111, Status: models.StatusActive, Secret: &secret, ProfileName: &name})
	fa := &fakeApplier{}
	a := testActions(fs, fa, false)

	u, err := a.Revoke(context.Background(), 111, "test-admin")
	if err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if u.Status != models.StatusRevoked {
		t.Errorf("status = %s, want revoked", u.Status)
	}
	if u.Secret != nil {
		t.Error("expected secret cleared")
	}
	if fa.RevokeCalls != 1 {
		t.Errorf("RevokeCalls = %d, want 1", fa.RevokeCalls)
	}
}

func TestRevokeNotActive(t *testing.T) {
	fs := newFakeStore()
	fs.addUser(&models.User{TelegramID: 111, Status: models.StatusPending})
	fa := &fakeApplier{}
	a := testActions(fs, fa, false)

	_, err := a.Revoke(context.Background(), 111, "test-admin")
	if !errors.Is(err, ErrNotActive) {
		t.Fatalf("err = %v, want ErrNotActive", err)
	}
}

func TestRevokeApplyFailureIsSurfacedNotHidden(t *testing.T) {
	fs := newFakeStore()
	secret := "abc"
	fs.addUser(&models.User{TelegramID: 111, Status: models.StatusActive, Secret: &secret})
	fa := &fakeApplier{RevokeErr: errForced}
	a := testActions(fs, fa, false)

	_, err := a.Revoke(context.Background(), 111, "test-admin")
	if !errors.Is(err, ErrRevokeApplyFailed) {
		t.Fatalf("err = %v, want ErrRevokeApplyFailed", err)
	}
	// DB is still updated to revoked per the documented tradeoff.
	got, _ := fs.GetUserByTelegramID(context.Background(), 111)
	if got.Status != models.StatusRevoked {
		t.Errorf("status = %s, want revoked even though apply failed", got.Status)
	}

	found := false
	for _, e := range fs.audit {
		if e.Action == auditActionRevokeApplyFail {
			found = true
		}
	}
	if !found {
		t.Error("expected a revoke_apply_failed audit entry")
	}
}

func TestDenyPending(t *testing.T) {
	fs := newFakeStore()
	fs.addUser(&models.User{TelegramID: 111, Status: models.StatusPending})
	fa := &fakeApplier{}
	a := testActions(fs, fa, false)

	u, err := a.Deny(context.Background(), 111, "test-admin")
	if err != nil {
		t.Fatalf("Deny: %v", err)
	}
	if u.Status != models.StatusDenied {
		t.Errorf("status = %s, want denied", u.Status)
	}
	if fa.IssueCalls != 0 || fa.RevokeCalls != 0 {
		t.Error("Deny must never touch the applier")
	}
}

func TestDenyNotPending(t *testing.T) {
	fs := newFakeStore()
	fs.addUser(&models.User{TelegramID: 111, Status: models.StatusActive})
	fa := &fakeApplier{}
	a := testActions(fs, fa, false)

	_, err := a.Deny(context.Background(), 111, "test-admin")
	if !errors.Is(err, ErrNotPending) {
		t.Fatalf("err = %v, want ErrNotPending", err)
	}
}
