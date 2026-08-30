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