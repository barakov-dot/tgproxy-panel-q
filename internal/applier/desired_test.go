package applier

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/barakov-dot/tgproxy-panel-q/internal/config"
	"github.com/barakov-dot/tgproxy-panel-q/internal/models"
	"github.com/barakov-dot/tgproxy-panel-q/internal/store"
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

func issueUser(t *testing.T, ctx context.Context, s *store.Store, tgID int64, profileName, secret string) {
	t.Helper()
	u, err := s.CreateUser(ctx, tgID, nil, nil, nil)
	if err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}
	if _, err := s.SetUserProfile(ctx, u.ID, profileName, secret, true); err != nil {
		t.Fatalf("SetUserProfile() error = %v", err)
	}
}

func revokeUser(t *testing.T, ctx context.Context, s *store.Store, tgID int64) {
	t.Helper()
	u, err := s.GetUserByTelegramID(ctx, tgID)
	if err != nil {
		t.Fatalf("GetUserByTelegramID() error = %v", err)
	}
	if _, err := s.UpdateUserStatus(ctx, u.ID, models.StatusRevoked); err != nil {
		t.Fatalf("UpdateUserStatus() error = %v", err)
	}
}

func TestDesiredProfiles(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name      string
		setup     func(t *testing.T, s *store.Store)
		wantCount int
	}{
		{
			name:      "empty",
			setup:     func(t *testing.T, s *store.Store) {},
			wantCount: 0,
		},
		{
			name: "only active",
			setup: func(t *testing.T, s *store.Store) {
				if _, err := s.CreateUser(ctx, 111, strPtr("pending"), nil, nil); err != nil {
					t.Fatal(err)
				}
				issueUser(t, ctx, s, 222, "user_222", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
				issueUser(t, ctx, s, 333, "user_333", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
				revokeUser(t, ctx, s, 333)
			},
			wantCount: 1,
		},
		{
			name: "multiple active share backend",
			setup: func(t *testing.T, s *store.Store) {
				for i, tgID := range []int64{1001, 1002, 1003} {
					issueUser(t, ctx, s, tgID, "user_"+strPtrHex(i)[:8], strPtrHex(i))
				}
			},
			wantCount: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := openTestStore(t)
			tt.setup(t, s)
			pf, err := desiredProfiles(ctx, Store{s}, testConfig())
			if err != nil {
				t.Fatalf("desiredProfiles() error = %v", err)
			}
			if len(pf.Profiles) != tt.wantCount {
				t.Fatalf("len(Profiles) = %d, want %d: %+v", len(pf.Profiles), tt.wantCount, pf.Profiles)
			}
			for _, p := range pf.Profiles {
				if p.Backend != "127.0.0.1:2398" || p.CarrierMode != "https" {
					t.Errorf("profile %q missing shared backend/carrier_mode: %+v", p.Name, p)
				}
			}
		})
	}
}

func strPtrHex(i int) string {
	digits := "0123456789abcdef"
	b := make([]byte, 32)
	for j := range b {
		b[j] = digits[(i+j)%16]
	}
	return string(b)
}
