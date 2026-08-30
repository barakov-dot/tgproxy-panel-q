// Package applier is the only package that touches live tproxy-server host state.
// It builds the panel-managed profile slice from the DB and hands it to the
// privileged apply-profiles.sh script via sudo, which merges it with any
// pre-existing non-panel profiles before writing profiles.json.
// See ARCHITECTURE.md and plan.md §7.
package applier

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

// panelProfileName matches profiles owned by tgproxy-panel (user_<telegram_id>).
var panelProfileName = regexp.MustCompile(`^user_\d+$`)

// IsPanelManagedProfile reports whether name is in the panel's namespace.
func IsPanelManagedProfile(name string) bool {
	return panelProfileName.MatchString(name)
}

// Profile mirrors one entry of profiles.json.
type Profile struct {
	Name        string `json:"name"`
	Secret      string `json:"secret"`
	Backend     string `json:"backend"`
	CarrierMode string `json:"carrier_mode"`
}

// ProfilesFile mirrors the top-level shape: {"profiles": [...]}.
type ProfilesFile struct {
	Profiles []Profile `json:"profiles"`
}

// Validate checks JSON-level invariants: non-empty fields and unique name/secret.
func (pf *ProfilesFile) Validate() error {
	names := make(map[string]struct{}, len(pf.Profiles))
	secrets := make(map[string]string, len(pf.Profiles))
	for _, p := range pf.Profiles {
		if p.Name == "" {
			return fmt.Errorf("applier: profile missing name")
		}
		if p.Secret == "" {
			return fmt.Errorf("applier: profile %q missing secret", p.Name)
		}
		if _, ok := names[p.Name]; ok {
			return fmt.Errorf("applier: duplicate profile name %q", p.Name)
		}
		if prev, ok := secrets[p.Secret]; ok {
			return fmt.Errorf("applier: secret for profile %q collides with existing profile %q", p.Name, prev)
		}
		names[p.Name] = struct{}{}
		secrets[p.Secret] = p.Name
	}
	return nil
}

// AddProfile appends p, rejecting collisions on name or secret.
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

// MergePanelProfiles keeps non-panel profiles from current and replaces all
// panel-managed entries (user_<telegram_id>) with those from panel.
func MergePanelProfiles(current, panel *ProfilesFile) (*ProfilesFile, error) {
	merged := &ProfilesFile{Profiles: make([]Profile, 0)}

	if current != nil {
		for _, p := range current.Profiles {
			if IsPanelManagedProfile(p.Name) {
				continue
			}
			if err := merged.AddProfile(p); err != nil {
				return nil, fmt.Errorf("applier: merge foreign profile %q: %w", p.Name, err)
			}
		}
	}

	if panel != nil {
		for _, p := range panel.Profiles {
			if err := merged.AddProfile(p); err != nil {
				return nil, fmt.Errorf("applier: merge panel profile %q: %w", p.Name, err)
			}
		}
	}

	return merged, nil
}

// RemoveProfile deletes the profile named name.
func (pf *ProfilesFile) RemoveProfile(name string) error {
	for i, p := range pf.Profiles {
		if p.Name == name {
			pf.Profiles = append(pf.Profiles[:i], pf.Profiles[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("applier: profile name %q not found", name)
}

// ReadProfiles loads and validates profiles.json from path.
func ReadProfiles(path string) (*ProfilesFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("applier: read profiles: %w", err)
	}
	var pf ProfilesFile
	if err := json.Unmarshal(data, &pf); err != nil {
		return nil, fmt.Errorf("applier: parse profiles JSON: %w", err)
	}
	if err := pf.Validate(); err != nil {
		return nil, err
	}
	return &pf, nil
}

// WriteProfiles atomically writes pf to path after validation.
func WriteProfiles(path string, pf *ProfilesFile) error {
	if err := pf.Validate(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(pf, "", "  ")
	if err != nil {
		return fmt.Errorf("applier: marshal profiles: %w", err)
	}
	data = append(data, '\n')

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("applier: create profiles dir: %w", err)
	}

	tmp, err := os.CreateTemp(dir, ".profiles-*.tmp")
	if err != nil {
		return fmt.Errorf("applier: create temp profiles file: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("applier: write temp profiles file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("applier: close temp profiles file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("applier: install profiles file: %w", err)
	}
	return nil
}
