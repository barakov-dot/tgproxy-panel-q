package applier

import (
	"context"
	"fmt"

	"github.com/barakov-dot/tgproxy-panel/internal/config"
	"github.com/barakov-dot/tgproxy-panel/internal/models"
)

// desiredProfiles reconstructs the complete profiles.json content that
// should exist on the host, purely from the DB: every user with
// status=active, joined with the one shared backend/carrier_mode from
// config (CLAUDE.md: "never allocate a new backend port per user"). This is
// the entire "desired state" computation — see the package doc comment for
// why it never reads the live file.
func desiredProfiles(ctx context.Context, s userLister, cfg *config.Config) (*ProfilesFile, error) {
	users, err := s.ListUsers(ctx)
	if err != nil {
		return nil, fmt.Errorf("applier: list users: %w", err)
	}

	pf := &ProfilesFile{Profiles: make([]Profile, 0, len(users))}
	for _, u := range users {
		if u.Status != models.StatusActive {
			continue
		}
		if u.ProfileName == nil || u.Secret == nil {
			return nil, fmt.Errorf("applier: user telegram_id=%d is active but missing profile_name/secret", u.TelegramID)
		}
		p := Profile{
			Name:        *u.ProfileName,
			Secret:      *u.Secret,
			Backend:     cfg.TproxyBackend,
			CarrierMode: cfg.TproxyCarrierMode,
		}
		if err := pf.AddProfile(p); err != nil {
			return nil, fmt.Errorf("applier: building desired state: %w", err)
		}
	}
	return pf, nil
}

// userLister is the slice of *store.Store this package depends on. Kept as
// a small interface so tests can exercise desiredProfiles without a real
// SQLite file, though internal/applier's tests currently use a real
// store.Store on a temp DB (cheap and exercises the real query).
type userLister interface {
	ListUsers(ctx context.Context) ([]*models.User, error)
}
