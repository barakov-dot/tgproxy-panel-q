// Package applier is the only package that touches the live tproxy-server
// host state. See CLAUDE.md's "Verified facts about tproxy-server" and
// plan.md §7 for the constraints this file is built against.
//
// Architecture: internal/applier is declarative, not diff-based. It never
// reads the live /etc/tproxy-server/profiles.json — it can't: that file is
// root:tproxy 0400, and this panel process runs unprivileged (plan.md §7
// "Права доступа"). Instead, every apply recomputes the *complete* desired
// profile list straight from the DB (all users with status=active already
// carry their profile_name/secret, per internal/store) and hands that whole
// list to the privileged deploy/apply-profiles.sh script via sudo. That
// script — a future stage, running as root — is the only thing that reads
// the current file, backs it up, validates, chowns/chmods, atomically
// renames, restarts tproxy-server and reports back. internal/applier's job
// stops at "produce a validated candidate file and ask the root script to
// install it."
package applier

import "fmt"

// Profile mirrors one entry of profiles.example.json's `profiles` array
// (CLAUDE.md's verified facts): a name, a 32-hex-char secret, and the
// shared MTProxy backend/carrier_mode every profile in this deployment
// uses.
type Profile struct {
	Name        string `json:"name"`
	Secret      string `json:"secret"`
	Backend     string `json:"backend"`
	CarrierMode string `json:"carrier_mode"`
}

// ProfilesFile mirrors the top-level shape of profiles.json:
// {"profiles": [...]}.
type ProfilesFile struct {
	Profiles []Profile `json:"profiles"`
}

// AddProfile appends p to pf, rejecting a collision on either Name or
// Secret so a bug upstream (duplicate telegram_id, secret reuse) fails
// loudly here instead of silently producing a profiles.json two users
// share.
func (pf *ProfilesFile) AddProfile(p Profile) error {
	for _, existing := range pf.Profiles {
		if existing.Name == p.Name {
			return fmt.Errorf("applier: profile name %q already exists", p.Name)
		}
		if existing.Secret == p.Secret {
			return fmt.Errorf("applier: secret for profile %q collides with existing profile %q", p.Name, existing.Name)
		}
	}
	pf.Profiles = append(pf.Profiles, p)
	return nil
}

// RemoveProfile deletes the profile named name, returning an error rather
// than silently no-op-ing if it isn't present.
func (pf *ProfilesFile) RemoveProfile(name string) error {
	for i, p := range pf.Profiles {
		if p.Name == name {
			pf.Profiles = append(pf.Profiles[:i], pf.Profiles[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("applier: profile name %q not found", name)
}
