package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/barakov-dot/tgproxy-panel/internal/models"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open() error: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func strPtr(s string) *string { return &s }

func TestOpen_CreatesSchema(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	users, err := s.ListUsers(ctx)
	if err != nil {
		t.Fatalf("ListUsers() on fresh DB: %v", err)
	}
	if len(users) != 0 {
		t.Errorf("expected no users on fresh DB, got %d", len(users))
	}

	if _, ok, err := s.GetSetting(ctx, "auto_issue"); err != nil || ok {
		t.Errorf("GetSetting on fresh DB: ok=%v err=%v, want ok=false err=nil", ok, err)
	}

	entries, err := s.ListAuditLog(ctx, 10)
	if err != nil {
		t.Fatalf("ListAuditLog() on fresh DB: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected no audit rows on fresh DB, got %d", len(entries))
	}
}

func TestCreateAndGetUser(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	created, err := s.CreateUser(ctx, 42, strPtr("adal"), strPtr("Ada"), strPtr("Lovelace"))
	if err != nil {
		t.Fatalf("CreateUser() error: %v", err)
	}
	if created.TelegramID != 42 {
		t.Errorf("TelegramID = %d, want 42", created.TelegramID)
	}
	if created.Status != models.StatusPending {
		t.Errorf("Status = %q, want pending", created.Status)
	}
	if created.RequestedAt == nil {
		t.Error("RequestedAt should be set")
	}
	if created.ProfileName != nil || created.Secret != nil {
		t.Error("ProfileName/Secret should be nil for a new pending user")
	}

	byID, err := s.GetUserByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetUserByID() error: %v", err)
	}
	if byID.TelegramID != 42 || *byID.Username != "adal" {
		t.Errorf("GetUserByID() = %+v, want telegram_id=42 username=adal", byID)
	}

	byTelegram, err := s.GetUserByTelegramID(ctx, 42)
	if err != nil {
		t.Fatalf("GetUserByTelegramID() error: %v", err)
	}
	if byTelegram.ID != created.ID {
		t.Errorf("GetUserByTelegramID() ID = %d, want %d", byTelegram.ID, created.ID)
	}
}

func TestGetUser_NotFound(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	if _, err := s.GetUserByID(ctx, 999); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetUserByID() error = %v, want ErrNotFound", err)
	}
	if _, err := s.GetUserByTelegramID(ctx, 999); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetUserByTelegramID() error = %v, want ErrNotFound", err)
	}
}

func TestCreateUser_DuplicateTelegramID(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	if _, err := s.CreateUser(ctx, 1, nil, nil, nil); err != nil {
		t.Fatalf("first CreateUser() error: %v", err)
	}
	if _, err := s.CreateUser(ctx, 1, nil, nil, nil); err == nil {
		t.Error("second CreateUser() with same telegram_id should fail (UNIQUE constraint)")
	}
}

func TestListUsers_MultipleUsers(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	for _, id := range []int64{1, 2, 3} {
		if _, err := s.CreateUser(ctx, id, nil, nil, nil); err != nil {
			t.Fatalf("CreateUser(%d) error: %v", id, err)
		}
	}

	users, err := s.ListUsers(ctx)
	if err != nil {
		t.Fatalf("ListUsers() error: %v", err)
	}
	if len(users) != 3 {
		t.Fatalf("ListUsers() returned %d users, want 3", len(users))
	}
}

func TestIssueUser(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	if _, err := s.CreateUser(ctx, 7, nil, nil, nil); err != nil {
		t.Fatalf("CreateUser() error: %v", err)
	}

	issued, err := s.IssueUser(ctx, 7, "user_7", "0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("IssueUser() error: %v", err)
	}
	if issued.Status != models.StatusActive {
		t.Errorf("Status = %q, want active", issued.Status)
	}
	if issued.ProfileName == nil || *issued.ProfileName != "user_7" {
		t.Errorf("ProfileName = %v, want user_7", issued.ProfileName)
	}
	if issued.Secret == nil || *issued.Secret != "0123456789abcdef0123456789abcdef" {
		t.Errorf("Secret = %v, want the issued secret", issued.Secret)
	}
	if issued.IssuedAt == nil {
		t.Error("IssuedAt should be set after IssueUser")
	}
	if !issued.IsActive() {
		t.Error("IsActive() should be true after IssueUser")
	}
}

func TestIssueUser_NotFound(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	if _, err := s.IssueUser(ctx, 999, "user_999", "secret"); !errors.Is(err, ErrNotFound) {
		t.Errorf("IssueUser() for unknown telegram_id error = %v, want ErrNotFound", err)
	}
}

func TestIssueUser_DuplicateProfileName(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	for _, id := range []int64{1, 2} {
		if _, err := s.CreateUser(ctx, id, nil, nil, nil); err != nil {
			t.Fatalf("CreateUser(%d) error: %v", id, err)
		}
	}
	if _, err := s.IssueUser(ctx, 1, "same_name", "secret1"); err != nil {
		t.Fatalf("IssueUser(1) error: %v", err)
	}
	if _, err := s.IssueUser(ctx, 2, "same_name", "secret2"); err == nil {
		t.Error("IssueUser() with a duplicate profile_name should fail (UNIQUE constraint)")
	}
}

func TestRevokeUser_ClearsProfileFields(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	if _, err := s.CreateUser(ctx, 7, nil, nil, nil); err != nil {
		t.Fatalf("CreateUser() error: %v", err)
	}
	if _, err := s.IssueUser(ctx, 7, "user_7", "0123456789abcdef0123456789abcdef"); err != nil {
		t.Fatalf("IssueUser() error: %v", err)
	}

	revoked, err := s.RevokeUser(ctx, 7)
	if err != nil {
		t.Fatalf("RevokeUser() error: %v", err)
	}
	if revoked.Status != models.StatusRevoked {
		t.Errorf("Status = %q, want revoked", revoked.Status)
	}
	if revoked.RevokedAt == nil {
		t.Error("RevokedAt should be set after RevokeUser")
	}
	if revoked.ProfileName != nil {
		t.Errorf("ProfileName = %v, want nil after revoke", revoked.ProfileName)
	}
	if revoked.Secret != nil {
		t.Errorf("Secret = %v, want nil after revoke", revoked.Secret)
	}
	if revoked.IsActive() {
		t.Error("IsActive() should be false after RevokeUser")
	}

	// A second user must now be free to reuse the same profile_name/secret
	// the revoked user held, proving the UNIQUE slots were actually freed.
	if _, err := s.CreateUser(ctx, 8, nil, nil, nil); err != nil {
		t.Fatalf("CreateUser(8) error: %v", err)
	}
	if _, err := s.IssueUser(ctx, 8, "user_7", "0123456789abcdef0123456789abcdef"); err != nil {
		t.Fatalf("IssueUser(8) reusing revoked profile_name/secret should succeed: %v", err)
	}
}

func TestRevokeUser_NotFound(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	if _, err := s.RevokeUser(ctx, 999); !errors.Is(err, ErrNotFound) {
		t.Errorf("RevokeUser() error = %v, want ErrNotFound", err)
	}
}

func TestDenyUser(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	if _, err := s.CreateUser(ctx, 3, nil, nil, nil); err != nil {
		t.Fatalf("CreateUser() error: %v", err)
	}
	denied, err := s.DenyUser(ctx, 3)
	if err != nil {
		t.Fatalf("DenyUser() error: %v", err)
	}
	if denied.Status != models.StatusDenied {
		t.Errorf("Status = %q, want denied", denied.Status)
	}
}

func TestSettings_GetSet(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	if _, ok, err := s.GetSetting(ctx, models.SettingAutoIssue); err != nil || ok {
		t.Fatalf("GetSetting() before set: ok=%v err=%v", ok, err)
	}

	if err := s.SetSetting(ctx, models.SettingAutoIssue, "true"); err != nil {
		t.Fatalf("SetSetting() error: %v", err)
	}
	value, ok, err := s.GetSetting(ctx, models.SettingAutoIssue)
	if err != nil || !ok || value != "true" {
		t.Fatalf("GetSetting() = %q, %v, %v; want true, true, nil", value, ok, err)
	}

	// Upsert should overwrite, not conflict.
	if err := s.SetSetting(ctx, models.SettingAutoIssue, "false"); err != nil {
		t.Fatalf("SetSetting() overwrite error: %v", err)
	}
	value, _, _ = s.GetSetting(ctx, models.SettingAutoIssue)
	if value != "false" {
		t.Errorf("value after overwrite = %q, want false", value)
	}
}

func TestAuditLog_WriteAndList(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	telegramID := int64(55)
	err := s.WriteAuditLog(ctx, models.AuditLog{
		Action:     "issue",
		TelegramID: &telegramID,
		Actor:      "admin",
		Detail:     "issued via panel",
	})
	if err != nil {
		t.Fatalf("WriteAuditLog() error: %v", err)
	}
	err = s.WriteAuditLog(ctx, models.AuditLog{
		Action: "settings_change",
		Actor:  "admin",
		Detail: "auto_issue -> true",
	})
	if err != nil {
		t.Fatalf("WriteAuditLog() error: %v", err)
	}

	entries, err := s.ListAuditLog(ctx, 10)
	if err != nil {
		t.Fatalf("ListAuditLog() error: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("ListAuditLog() returned %d entries, want 2", len(entries))
	}
	// Newest first.
	if entries[0].Action != "settings_change" {
		t.Errorf("entries[0].Action = %q, want settings_change", entries[0].Action)
	}
	if entries[1].TelegramID == nil || *entries[1].TelegramID != 55 {
		t.Errorf("entries[1].TelegramID = %v, want 55", entries[1].TelegramID)
	}
	for _, e := range entries {
		if e.Detail == "0123456789abcdef0123456789abcdef" {
			t.Error("audit log detail must never contain a raw secret")
		}
	}
}
