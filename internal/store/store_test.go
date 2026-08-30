package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/barakov-dot/tgproxy-panel-q/internal/models"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open() error: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func strPtr(s string) *string { return &s }

func TestOpen_CreatesSchema(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	users, err := s.ListUsers(ctx, UserListFilter{}, UserListSort{})
	if err != nil {
		t.Fatalf("ListUsers() on fresh DB: %v", err)
	}
	if len(users) != 0 {
		t.Errorf("expected no users, got %d", len(users))
	}

	if _, ok, err := s.GetSetting(ctx, models.SettingAutoIssue); err != nil || ok {
		t.Errorf("GetSetting on fresh DB: ok=%v err=%v", ok, err)
	}

	entries, err := s.ListAuditLog(ctx, 10)
	if err != nil {
		t.Fatalf("ListAuditLog() on fresh DB: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected no audit rows, got %d", len(entries))
	}
}

func TestCreateAndGetUser(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	created, err := s.CreateUser(ctx, 42, strPtr("adal"), strPtr("Ada"), strPtr("Lovelace"))
	if err != nil {
		t.Fatalf("CreateUser() error: %v", err)
	}
	if created.Status != models.StatusPending {
		t.Errorf("Status = %q, want pending", created.Status)
	}
	if created.RequestedAt == nil {
		t.Error("RequestedAt should be set")
	}

	byID, err := s.GetUserByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetUserByID() error: %v", err)
	}
	if byID.TelegramID != 42 || *byID.Username != "adal" {
		t.Errorf("GetUserByID() = %+v", byID)
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
		t.Error("duplicate telegram_id should fail")
	}
}

func TestListUsers_FilterAndSort(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	u1, _ := s.CreateUser(ctx, 1, strPtr("alice"), nil, nil)
	u2, _ := s.CreateUser(ctx, 2, strPtr("bob"), nil, nil)
	if _, err := s.SetUserProfile(ctx, u2.ID, "user_2", "0123456789abcdef0123456789abcdef", true); err != nil {
		t.Fatal(err)
	}

	pending := models.StatusPending
	pendingUsers, err := s.ListUsers(ctx, UserListFilter{Status: &pending}, UserListSort{Column: "telegram_id"})
	if err != nil {
		t.Fatal(err)
	}
	if len(pendingUsers) != 1 || pendingUsers[0].ID != u1.ID {
		t.Errorf("pending filter: got %+v, want user %d", pendingUsers, u1.ID)
	}

	searchUsers, err := s.ListUsers(ctx, UserListFilter{Query: "bob"}, UserListSort{})
	if err != nil {
		t.Fatal(err)
	}
	if len(searchUsers) != 1 || searchUsers[0].TelegramID != 2 {
		t.Errorf("search filter: got %d users", len(searchUsers))
	}

	sorted, err := s.ListUsers(ctx, UserListFilter{}, UserListSort{Column: "telegram_id", Desc: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(sorted) != 2 || sorted[0].TelegramID != 2 {
		t.Errorf("sort desc telegram_id: first = %d", sorted[0].TelegramID)
	}
}

func TestSetUserProfile(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	u, err := s.CreateUser(ctx, 7, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	issued, err := s.SetUserProfile(ctx, u.ID, "user_7", "0123456789abcdef0123456789abcdef", true)
	if err != nil {
		t.Fatalf("SetUserProfile() error: %v", err)
	}
	if issued.Status != models.StatusActive {
		t.Errorf("Status = %q, want active", issued.Status)
	}
	if issued.ProfileName == nil || *issued.ProfileName != "user_7" {
		t.Errorf("ProfileName = %v", issued.ProfileName)
	}
	if issued.IssuedAt == nil {
		t.Error("IssuedAt should be set")
	}
}

func TestUpdateUserStatus_RevokeClearsProfile(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	u, _ := s.CreateUser(ctx, 7, nil, nil, nil)
	if _, err := s.SetUserProfile(ctx, u.ID, "user_7", "0123456789abcdef0123456789abcdef", true); err != nil {
		t.Fatal(err)
	}

	revoked, err := s.UpdateUserStatus(ctx, u.ID, models.StatusRevoked)
	if err != nil {
		t.Fatalf("UpdateUserStatus() error: %v", err)
	}
	if revoked.Status != models.StatusRevoked {
		t.Errorf("Status = %q, want revoked", revoked.Status)
	}
	if revoked.ProfileName != nil || revoked.Secret != nil {
		t.Error("profile fields should be cleared on revoke")
	}
	if revoked.RevokedAt == nil {
		t.Error("RevokedAt should be set")
	}
}

func TestUpdateUserStatus_Invalid(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	u, _ := s.CreateUser(ctx, 1, nil, nil, nil)
	if _, err := s.UpdateUserStatus(ctx, u.ID, models.UserStatus("nope")); err == nil {
		t.Error("expected error for invalid status")
	}
}

func TestSettings_GetSet(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	if _, ok, err := s.GetSetting(ctx, models.SettingAutoIssue); err != nil || ok {
		t.Fatalf("GetSetting before set: ok=%v err=%v", ok, err)
	}

	if err := s.SetSetting(ctx, models.SettingAutoIssue, "true"); err != nil {
		t.Fatal(err)
	}
	value, ok, err := s.GetSetting(ctx, models.SettingAutoIssue)
	if err != nil || !ok || value != "true" {
		t.Fatalf("GetSetting() = %q ok=%v err=%v", value, ok, err)
	}

	if err := s.SetSetting(ctx, models.SettingAutoIssue, "false"); err != nil {
		t.Fatal(err)
	}
	value, _, _ = s.GetSetting(ctx, models.SettingAutoIssue)
	if value != "false" {
		t.Errorf("value after overwrite = %q", value)
	}
}

func TestAuditLog_AppendAndList(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	u, _ := s.CreateUser(ctx, 55, nil, nil, nil)
	userID := u.ID

	if err := s.AppendAuditLog(ctx, models.AuditLog{
		Action: "issue",
		Actor:  "admin",
		UserID: &userID,
		Detail: "issued via panel",
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendAuditLog(ctx, models.AuditLog{
		Action: "settings_change",
		Actor:  "admin",
		Detail: "auto_issue -> true",
	}); err != nil {
		t.Fatal(err)
	}

	entries, err := s.ListAuditLog(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("ListAuditLog() returned %d entries, want 2", len(entries))
	}
	if entries[0].Action != "settings_change" {
		t.Errorf("newest action = %q", entries[0].Action)
	}
	if entries[1].UserID == nil || *entries[1].UserID != userID {
		t.Errorf("entries[1].UserID = %v, want %d", entries[1].UserID, userID)
	}
}
