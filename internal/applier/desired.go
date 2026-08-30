package applier

import (
	"context"
	"fmt"

	"github.com/barakov-dot/tgproxy-panel-q/internal/config"
	"github.com/barakov-dot/tgproxy-panel-q/internal/models"
)

// desiredProfiles builds the panel-managed slice of profiles.json (active DB
// users only). deploy/apply-profiles.sh merges this with any existing
// non-panel profiles (e.g. "default") before installing the live file.
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

type userLister interface {
	ListUsers(ctx context.Context) ([]*models.User, error)
}
