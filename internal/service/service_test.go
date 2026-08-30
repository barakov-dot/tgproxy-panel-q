package service

import (
	"context"
	"errors"
	"testing"

	"github.com/barakov-dot/tgproxy-panel-q/internal/models"
	"github.com/barakov-dot/tgproxy-panel-q/internal/store"
)

func TestIssue(t *testing.T) {
	tests := []struct {
		name           string
		setup          func(*fakeStore)
		telegramID     int64
		issueErr       error
		wantErr        error
		wantStatus     models.UserStatus
		wantIssueCalls int
		wantAudit      string
		idempotent     bool
	}{
		{
			name: "issues pending user",
			setup: func(fs *fakeStore) {
				fs.addUser(&models.User{TelegramID: 111, Status: models.StatusPending})
			},
			telegramID:     111,
			wantStatus:     models.StatusActive,
			wantIssueCalls: 1,
			wantAudit:      auditActionIssue,
		},
		{
			name: "idempotent for active user",
			setup: func(fs *fakeStore) {
				secret := "abc123"
				name := "user_222"
				fs.addUser(&models.User{
					TelegramID: 222, Status: models.StatusActive,
					Secret: &secret, ProfileName: &name,
				})
			},
			telegramID:     222,
			wantStatus:     models.StatusActive,
			wantIssueCalls: 0,
			idempotent:     true,
		},
		{
			name: "rolls back on applier failure",
			setup: func(fs *fakeStore) {
				fs.addUser(&models.User{TelegramID: 333, Status: models.StatusPending})
			},
			telegramID:     333,
			issueErr:       errForced,
			wantErr:        ErrIssueFailed,
			wantStatus:     models.StatusPending,
			wantIssueCalls: 1,
			wantAudit:      auditActionIssueRolledBack,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := newFakeStore()
			tt.setup(fs)
			fa := &fakeApplier{IssueErr: tt.issueErr}
			svc := testService(fs, fa, nil, false)
			ctx := context.Background()

			u, _ := fs.GetUserByTelegramID(ctx, tt.telegramID)
			got, err := svc.Issue(ctx, u, "test-admin")

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("Issue() err = %v, want %v", err, tt.wantErr)
				}
			} else if err != nil {
				t.Fatalf("Issue() err = %v", err)
			}

			if tt.idempotent && got != nil && u.Secret != nil && got.Secret != nil && *got.Secret != *u.Secret {
				t.Errorf("idempotent Issue changed secret")
			}

			after, lookupErr := fs.GetUserByTelegramID(ctx, tt.telegramID)
			if lookupErr != nil {
				t.Fatalf("lookup: %v", lookupErr)
			}
			if after.Status != tt.wantStatus {
				t.Errorf("status = %s, want %s", after.Status, tt.wantStatus)
			}
			if fa.IssueCalls != tt.wantIssueCalls {
				t.Errorf("IssueCalls = %d, want %d", fa.IssueCalls, tt.wantIssueCalls)
			}
			if tt.wantAudit != "" && !hasAuditAction(fs, tt.wantAudit) {
				t.Errorf("expected audit action %q, got %+v", tt.wantAudit, fs.audit)
			}
			if auditHasSecret(fs) {
				t.Error("audit log must not contain secrets")
			}
		})
	}
}

func TestApprove(t *testing.T) {
	tests := []struct {
		name       string
		setup      func(*fakeStore) int64
		wantErr    error
		wantActive bool
	}{
		{
			name: "approves pending",
			setup: func(fs *fakeStore) int64 {
				u := fs.addUser(&models.User{TelegramID: 111, Status: models.StatusPending})
				return u.ID
			},
			wantActive: true,
		},
		{
			name: "already active",
			setup: func(fs *fakeStore) int64 {
				u := fs.addUser(&models.User{TelegramID: 112, Status: models.StatusActive})
				return u.ID
			},
			wantErr: ErrAlreadyActive,
		},
		{
			name: "not pending",
			setup: func(fs *fakeStore) int64 {
				u := fs.addUser(&models.User{TelegramID: 113, Status: models.StatusDenied})
				return u.ID
			},
			wantErr: ErrNotPending,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := newFakeStore()
			userID := tt.setup(fs)
			fa := &fakeApplier{}
			svc := testService(fs, fa, nil, false)

			u, err := svc.Approve(context.Background(), userID, "admin")
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("Approve() err = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Approve() err = %v", err)
			}
			if tt.wantActive && u.Status != models.StatusActive {
				t.Errorf("status = %s, want active", u.Status)
			}
		})
	}
}

func TestRevoke(t *testing.T) {
	tests := []struct {
		name        string
		setup       func(*fakeStore) int64
		revokeErr   error
		wantErr     error
		wantRevoked bool
		wantAudit   string
	}{
		{
			name: "revokes active user",
			setup: func(fs *fakeStore) int64 {
				secret := "abc"
				name := "user_111"
				u := fs.addUser(&models.User{
					TelegramID: 111, Status: models.StatusActive,
					Secret: &secret, ProfileName: &name,
				})
				return u.ID
			},
			wantRevoked: true,
			wantAudit:   auditActionRevoke,
		},
		{
			name: "not active",
			setup: func(fs *fakeStore) int64 {
				u := fs.addUser(&models.User{TelegramID: 112, Status: models.StatusPending})
				return u.ID
			},
			wantErr: ErrNotActive,
		},
		{
			name: "apply failure surfaced",
			setup: func(fs *fakeStore) int64 {
				secret := "abc"
				u := fs.addUser(&models.User{TelegramID: 113, Status: models.StatusActive, Secret: &secret})
				return u.ID
			},
			revokeErr:   errForced,
			wantErr:     ErrRevokeApplyFailed,
			wantRevoked: true,
			wantAudit:   auditActionRevokeApplyFail,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := newFakeStore()
			userID := tt.setup(fs)
			fa := &fakeApplier{RevokeErr: tt.revokeErr}
			svc := testService(fs, fa, nil, false)
			ctx := context.Background()

			_, err := svc.Revoke(ctx, userID, "admin")
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("Revoke() err = %v, want %v", err, tt.wantErr)
				}
			} else if err != nil {
				t.Fatalf("Revoke() err = %v", err)
			}

			u, _ := fs.GetUserByID(ctx, userID)
			if tt.wantRevoked && u.Status != models.StatusRevoked {
				t.Errorf("status = %s, want revoked", u.Status)
			}
			if tt.wantAudit != "" && !hasAuditAction(fs, tt.wantAudit) {
				t.Errorf("expected audit %q", tt.wantAudit)
			}
		})
	}
}

func TestDeny(t *testing.T) {
	fs := newFakeStore()
	u := fs.addUser(&models.User{TelegramID: 111, Status: models.StatusPending})
	fa := &fakeApplier{}
	svc := testService(fs, fa, nil, false)

	denied, err := svc.Deny(context.Background(), u.ID, "admin")
	if err != nil {
		t.Fatalf("Deny() err = %v", err)
	}
	if denied.Status != models.StatusDenied {
		t.Errorf("status = %s, want denied", denied.Status)
	}
	if fa.IssueCalls != 0 || fa.RevokeCalls != 0 {
		t.Error("Deny must not touch applier")
	}
	if !hasAuditAction(fs, auditActionDeny) {
		t.Error("expected deny audit entry")
	}

	_, err = svc.Deny(context.Background(), 999999, "admin")
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("Deny unknown user err = %v, want ErrNotFound", err)
	}
}

func TestRequest(t *testing.T) {
	tests := []struct {
		name           string
		setup          func(*fakeStore)
		telegramID     int64
		defaultAuto    bool
		setting        string
		settingSet     bool
		issueErr       error
		wantOutcome    RequestOutcome
		wantErr        error
		wantIssueCalls int
	}{
		{
			name:           "new user auto issue on",
			telegramID:     100,
			defaultAuto:    true,
			wantOutcome:    OutcomeIssued,
			wantIssueCalls: 1,
		},
		{
			name:           "new user auto issue off",
			telegramID:     101,
			defaultAuto:    false,
			wantOutcome:    OutcomePendingCreated,
			wantIssueCalls: 0,
		},
		{
			name:           "setting overrides default",
			telegramID:     102,
			defaultAuto:    false,
			setting:        "true",
			settingSet:     true,
			wantOutcome:    OutcomeIssued,
			wantIssueCalls: 1,
		},
		{
			name:           "already active",
			telegramID:     103,
			defaultAuto:    true,
			setup: func(fs *fakeStore) {
				secret := "keep"
				fs.addUser(&models.User{TelegramID: 103, Status: models.StatusActive, Secret: &secret})
			},
			wantOutcome:    OutcomeAlreadyActive,
			wantIssueCalls: 0,
		},
		{
			name:           "already pending",
			telegramID:     104,
			defaultAuto:    false,
			setup: func(fs *fakeStore) {
				fs.addUser(&models.User{TelegramID: 104, Status: models.StatusPending})
			},
			wantOutcome:    OutcomeAlreadyPending,
			wantIssueCalls: 0,
		},
		{
			name:        "applier failure rolls back",
			telegramID:  105,
			defaultAuto: true,
			issueErr:    errForced,
			wantErr:     ErrIssueFailed,
		},
		{
			name:        "reopens revoked as pending",
			telegramID:  106,
			defaultAuto: false,
			setup: func(fs *fakeStore) {
				fs.addUser(&models.User{TelegramID: 106, Status: models.StatusRevoked})
			},
			wantOutcome:    OutcomePendingCreated,
			wantIssueCalls: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := newFakeStore()
			if tt.setup != nil {
				tt.setup(fs)
			}
			if tt.settingSet {
				fs.settings[models.SettingAutoIssue] = tt.setting
			}
			fa := &fakeApplier{IssueErr: tt.issueErr}
			svc := testService(fs, fa, nil, tt.defaultAuto)
			ctx := context.Background()

			res, err := svc.Request(ctx, tt.telegramID, nil, nil, nil)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("Request() err = %v, want %v", err, tt.wantErr)
				}
				if tt.telegramID == 105 {
					u, _ := fs.GetUserByTelegramID(ctx, tt.telegramID)
					if u.Status == models.StatusActive {
						t.Fatal("user left active after failed apply")
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("Request() err = %v", err)
			}
			if res.Outcome != tt.wantOutcome {
				t.Errorf("Outcome = %v, want %v", res.Outcome, tt.wantOutcome)
			}
			if fa.IssueCalls != tt.wantIssueCalls {
				t.Errorf("IssueCalls = %d, want %d", fa.IssueCalls, tt.wantIssueCalls)
			}
		})
	}
}

func TestGetProxyLinkAndResend(t *testing.T) {
	secret := "deadbeefdeadbeefdeadbeefdeadbeef"
	u := &models.User{TelegramID: 42, Status: models.StatusActive, Secret: &secret}
	fs := newFakeStore()
	fs.addUser(u)
	fa := &fakeApplier{}
	bot := &fakeBotSender{}
	svc := testService(fs, fa, bot, false)

	link := svc.GetProxyLink(u)
	if link == "" {
		t.Fatal("GetProxyLink returned empty")
	}
	if containsSecret(link, secret) && !findSubstring(link, "secret=") {
		t.Error("expected secret in proxy link")
	}

	err := svc.Resend(context.Background(), u.ID, "admin")
	if err != nil {
		t.Fatalf("Resend() err = %v", err)
	}
	if len(bot.calls) != 1 || bot.calls[0].TelegramID != 42 {
		t.Errorf("bot calls = %+v", bot.calls)
	}
	if !hasAuditAction(fs, auditActionResend) {
		t.Error("expected resend audit entry")
	}
}

func TestResendErrors(t *testing.T) {
	fs := newFakeStore()
	u := fs.addUser(&models.User{TelegramID: 1, Status: models.StatusPending})
	svc := testService(fs, &fakeApplier{}, nil, false)

	if err := svc.Resend(context.Background(), u.ID, "admin"); !errors.Is(err, ErrNotActive) {
		t.Errorf("pending resend err = %v, want ErrNotActive", err)
	}

	svcNoBot := testService(fs, &fakeApplier{}, nil, false)
	secret := "abc"
	active := fs.addUser(&models.User{TelegramID: 2, Status: models.StatusActive, Secret: &secret})
	if err := svcNoBot.Resend(context.Background(), active.ID, "admin"); !errors.Is(err, ErrNoBotSender) {
		t.Errorf("no bot err = %v, want ErrNoBotSender", err)
	}
}

func TestAutoIssueEnabled(t *testing.T) {
	fs := newFakeStore()
	svc := testService(fs, &fakeApplier{}, nil, true)

	enabled, err := svc.AutoIssueEnabled(context.Background())
	if err != nil || !enabled {
		t.Fatalf("default AutoIssueEnabled = (%v, %v), want (true, nil)", enabled, err)
	}

	fs.settings[models.SettingAutoIssue] = "false"
	enabled, err = svc.AutoIssueEnabled(context.Background())
	if err != nil || enabled {
		t.Fatalf("setting override = (%v, %v), want (false, nil)", enabled, err)
	}
}

func TestApproveDenyFlow(t *testing.T) {
	fs := newFakeStore()
	fa := &fakeApplier{}
	svc := testService(fs, fa, nil, false)
	ctx := context.Background()

	res, err := svc.Request(ctx, 107, nil, nil, nil)
	if err != nil {
		t.Fatalf("Request() err = %v", err)
	}
	userID := res.User.ID

	u, err := svc.Approve(ctx, userID, ActorAdmin(999))
	if err != nil {
		t.Fatalf("Approve() err = %v", err)
	}
	if u.Status != models.StatusActive {
		t.Errorf("status = %s, want active", u.Status)
	}

	fs2 := newFakeStore()
	svc2 := testService(fs2, &fakeApplier{}, nil, false)
	res2, _ := svc2.Request(ctx, 108, nil, nil, nil)
	denied, err := svc2.Deny(ctx, res2.User.ID, ActorAdmin(999))
	if err != nil {
		t.Fatalf("Deny() err = %v", err)
	}
	if denied.Status != models.StatusDenied {
		t.Errorf("status = %s, want denied", denied.Status)
	}
}
