package applier

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/barakov-dot/tgproxy-panel/internal/config"
	"github.com/barakov-dot/tgproxy-panel/internal/store"
)

func openTestStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func strPtr(s string) *string { return &s }

func testConfig() *config.Config {
	return &config.Config{
		TproxyBackend:     "127.0.0.1:2398",
		TproxyCarrierMode: "https",
	}
}

func TestDesiredProfiles_OnlyActiveUsers(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	// pending: not yet issued, must not appear.
	if _, err := s.CreateUser(ctx, 111, strPtr("pending_user"), nil, nil); err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}

	// active: must appear.
	if _, err := s.CreateUser(ctx, 222, strPtr("active_user"), nil, nil); err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}
	if _, err := s.IssueUser(ctx, 222, "user_222", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"); err != nil {
		t.Fatalf("IssueUser() error = %v", err)
	}

	// revoked: must not appear.
	if _, err := s.CreateUser(ctx, 333, strPtr("revoked_user"), nil, nil); err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}
	if _, err := s.IssueUser(ctx, 333, "user_333", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"); err != nil {
		t.Fatalf("IssueUser() error = %v", err)
	}
	if _, err := s.RevokeUser(ctx, 333); err != nil {
		t.Fatalf("RevokeUser() error = %v", err)
	}

	pf, err := desiredProfiles(ctx, s, testConfig())
	if err != nil {
		t.Fatalf("desiredProfiles() error = %v", err)
	}
	if len(pf.Profiles) != 1 {
		t.Fatalf("len(Profiles) = %d, want 1: %+v", len(pf.Profiles), pf.Profiles)
	}
	p := pf.Profiles[0]
	if p.Name != "user_222" || p.Secret != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Errorf("unexpected profile: %+v", p)
	}
	if p.Backend != "127.0.0.1:2398" || p.CarrierMode != "https" {
		t.Errorf("backend/carrier_mode not taken from config: %+v", p)
	}
}

func TestDesiredProfiles_EmptyWhenNoActiveUsers(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	pf, err := desiredProfiles(ctx, s, testConfig())
	if err != nil {
		t.Fatalf("desiredProfiles() error = %v", err)
	}
	if len(pf.Profiles) != 0 {
		t.Errorf("len(Profiles) = %d, want 0", len(pf.Profiles))
	}
}

func TestDesiredProfiles_MultipleActiveUsersShareBackend(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	tgIDs := []int64{1001, 1002, 1003}
	for i, tgID := range tgIDs {
		if _, err := s.CreateUser(ctx, tgID, nil, nil, nil); err != nil {
			t.Fatalf("CreateUser() error = %v", err)
		}
		profileName := "user_" + strPtrHex(i)[:8]
		if _, err := s.IssueUser(ctx, tgID, profileName, strPtrHex(i)); err != nil {
			t.Fatalf("IssueUser() error = %v", err)
		}
	}

	pf, err := desiredProfiles(ctx, s, testConfig())
	if err != nil {
		t.Fatalf("desiredProfiles() error = %v", err)
	}
	if len(pf.Profiles) != len(tgIDs) {
		t.Fatalf("len(Profiles) = %d, want %d: %+v", len(pf.Profiles), len(tgIDs), pf.Profiles)
	}
	for _, p := range pf.Profiles {
		if p.Backend != "127.0.0.1:2398" || p.CarrierMode != "https" {
			t.Errorf("profile %q does not share config backend/carrier_mode: %+v", p.Name, p)
		}
	}
}

// strPtrHex returns a distinct 32-hex-char string for index i, for tests
// that need multiple unique secrets without caring about their exact
// value.
func strPtrHex(i int) string {
	digits := "0123456789abcdef"
	b := make([]byte, 32)
	for j := range b {
		b[j] = digits[(i+j)%16]
	}
	return string(b)
}
