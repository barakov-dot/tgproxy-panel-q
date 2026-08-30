package applier

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestProfilesFile_JSONShape(t *testing.T) {
	pf := ProfilesFile{Profiles: []Profile{
		{Name: "user_1", Secret: "0123456789abcdef0123456789abcdef", Backend: "127.0.0.1:2398", CarrierMode: "https"},
	}}
	data, err := json.Marshal(pf)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	want := `{"profiles":[{"name":"user_1","secret":"0123456789abcdef0123456789abcdef","backend":"127.0.0.1:2398","carrier_mode":"https"}]}`
	if string(data) != want {
		t.Errorf("Marshal() = %s, want %s", data, want)
	}
}

func TestProfilesFile_UnmarshalMatchesUpstreamExample(t *testing.T) {
	const example = `{
	  "profiles": [
	    {
	      "name": "default",
	      "secret": "000102030405060708090a0b0c0d0e0f",
	      "backend": "127.0.0.1:2398",
	      "carrier_mode": "https"
	    }
	  ]
	}`
	var pf ProfilesFile
	if err := json.Unmarshal([]byte(example), &pf); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if len(pf.Profiles) != 1 {
		t.Fatalf("len(Profiles) = %d, want 1", len(pf.Profiles))
	}
	p := pf.Profiles[0]
	if p.Name != "default" || p.Secret != "000102030405060708090a0b0c0d0e0f" ||
		p.Backend != "127.0.0.1:2398" || p.CarrierMode != "https" {
		t.Errorf("unexpected profile: %+v", p)
	}
}

func TestProfilesFile_Validate(t *testing.T) {
	tests := []struct {
		name    string
		pf      ProfilesFile
		wantErr string
	}{
		{
			name: "valid empty",
			pf:   ProfilesFile{},
		},
		{
			name: "valid single",
			pf: ProfilesFile{Profiles: []Profile{
				{Name: "user_1", Secret: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Backend: "127.0.0.1:2398", CarrierMode: "https"},
			}},
		},
		{
			name: "missing name",
			pf: ProfilesFile{Profiles: []Profile{
				{Secret: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
			}},
			wantErr: "missing name",
		},
		{
			name: "missing secret",
			pf: ProfilesFile{Profiles: []Profile{
				{Name: "user_1"},
			}},
			wantErr: "missing secret",
		},
		{
			name: "duplicate name",
			pf: ProfilesFile{Profiles: []Profile{
				{Name: "user_1", Secret: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
				{Name: "user_1", Secret: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
			}},
			wantErr: "duplicate profile name",
		},
		{
			name: "duplicate secret",
			pf: ProfilesFile{Profiles: []Profile{
				{Name: "user_1", Secret: "sharedsecretsharedsecretsharedsec"},
				{Name: "user_2", Secret: "sharedsecretsharedsecretsharedsec"},
			}},
			wantErr: "collides",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.pf.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() error = %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("Validate() expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Validate() error = %q, want substring %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestReadWriteProfiles(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/profiles.json"
	pf := &ProfilesFile{Profiles: []Profile{
		{Name: "user_1", Secret: "0123456789abcdef0123456789abcdef", Backend: "127.0.0.1:2398", CarrierMode: "https"},
	}}

	if err := WriteProfiles(path, pf); err != nil {
		t.Fatalf("WriteProfiles() error = %v", err)
	}
	got, err := ReadProfiles(path)
	if err != nil {
		t.Fatalf("ReadProfiles() error = %v", err)
	}
	if len(got.Profiles) != 1 || got.Profiles[0].Name != "user_1" {
		t.Errorf("ReadProfiles() = %+v", got)
	}
}

func TestReadProfiles_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/bad.json"
	if err := WriteProfiles(path, &ProfilesFile{}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := ReadProfiles(path)
	if err == nil {
		t.Fatal("ReadProfiles() expected error for invalid JSON")
	}
	if !strings.Contains(err.Error(), "parse profiles JSON") {
		t.Errorf("error = %q", err.Error())
	}
}

func TestIsPanelManagedProfile(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"user_123456789", true},
		{"user_1", true},
		{"default", false},
		{"user_", false},
		{"user_abc", false},
		{"admin", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsPanelManagedProfile(tt.name); got != tt.want {
				t.Errorf("IsPanelManagedProfile(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestMergePanelProfiles(t *testing.T) {
	defaultProfile := Profile{
		Name: "default", Secret: "000102030405060708090a0b0c0d0e0f",
		Backend: "127.0.0.1:2398", CarrierMode: "https",
	}
	panelProfile := Profile{
		Name: "user_93455874", Secret: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Backend: "127.0.0.1:2398", CarrierMode: "https",
	}
	stalePanel := Profile{
		Name: "user_999", Secret: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		Backend: "127.0.0.1:2398", CarrierMode: "https",
	}

	tests := []struct {
		name        string
		current     *ProfilesFile
		panel       *ProfilesFile
		want        []string
		wantBackend string
	}{
		{
			name:    "panel only on empty current",
			current: nil,
			panel:   &ProfilesFile{Profiles: []Profile{panelProfile}},
			want:    []string{"user_93455874"},
		},
		{
			name:    "preserves default and adds panel user",
			current: &ProfilesFile{Profiles: []Profile{defaultProfile}},
			panel:   &ProfilesFile{Profiles: []Profile{panelProfile}},
			want:    []string{"default", "user_93455874"},
		},
		{
			name: "panel profile inherits backend from default",
			current: &ProfilesFile{Profiles: []Profile{{
				Name: "default", Secret: "000102030405060708090a0b0c0d0e0f",
				Backend: "127.0.0.1:2401", CarrierMode: "https",
			}}},
			panel: &ProfilesFile{Profiles: []Profile{{
				Name: "user_1", Secret: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				Backend: "127.0.0.1:2398", CarrierMode: "https",
			}}},
			want:        []string{"default", "user_1"},
			wantBackend: "127.0.0.1:2401",
		},
		{
			name:    "replaces stale panel profile keeps default",
			current: &ProfilesFile{Profiles: []Profile{defaultProfile, stalePanel}},
			panel:   &ProfilesFile{Profiles: []Profile{panelProfile}},
			want:    []string{"default", "user_93455874"},
		},
		{
			name:    "no panel users keeps foreign only",
			current: &ProfilesFile{Profiles: []Profile{defaultProfile, stalePanel}},
			panel:   &ProfilesFile{},
			want:    []string{"default"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := MergePanelProfiles(tt.current, tt.panel)
			if err != nil {
				t.Fatalf("MergePanelProfiles() error = %v", err)
			}
			if len(got.Profiles) != len(tt.want) {
				t.Fatalf("len = %d, want %d: %+v", len(got.Profiles), len(tt.want), got.Profiles)
			}
			for i, name := range tt.want {
				if got.Profiles[i].Name != name {
					t.Errorf("profile[%d].Name = %q, want %q", i, got.Profiles[i].Name, name)
				}
			}
			if tt.wantBackend != "" {
				for _, p := range got.Profiles {
					if IsPanelManagedProfile(p.Name) && p.Backend != tt.wantBackend {
						t.Errorf("panel profile %q backend = %q, want %q", p.Name, p.Backend, tt.wantBackend)
					}
				}
			}
		})
	}
}