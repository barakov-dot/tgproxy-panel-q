package applier

import (
	"encoding/json"
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
	got := string(data)
	want := `{"profiles":[{"name":"user_1","secret":"0123456789abcdef0123456789abcdef","backend":"127.0.0.1:2398","carrier_mode":"https"}]}`
	if got != want {
		t.Errorf("Marshal() = %s, want %s", got, want)
	}
}

func TestProfilesFile_UnmarshalMatchesUpstreamExample(t *testing.T) {
	// Matches profiles.example.json verbatim (CLAUDE.md verified facts).
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

func TestAddProfile_Success(t *testing.T) {
	var pf ProfilesFile
	if err := pf.AddProfile(Profile{Name: "user_1", Secret: "a"}); err != nil {
		t.Fatalf("AddProfile() error = %v", err)
	}
	if err := pf.AddProfile(Profile{Name: "user_2", Secret: "b"}); err != nil {
		t.Fatalf("AddProfile() error = %v", err)
	}
	if len(pf.Profiles) != 2 {
		t.Fatalf("len(Profiles) = %d, want 2", len(pf.Profiles))
	}
}

func TestAddProfile_NameCollision(t *testing.T) {
	var pf ProfilesFile
	must(t, pf.AddProfile(Profile{Name: "user_1", Secret: "a"}))
	err := pf.AddProfile(Profile{Name: "user_1", Secret: "b"})
	if err == nil {
		t.Fatal("AddProfile() with duplicate name: expected error, got nil")
	}
	if !strings.Contains(err.Error(), "user_1") {
		t.Errorf("error %q does not mention colliding name", err.Error())
	}
	if len(pf.Profiles) != 1 {
		t.Errorf("len(Profiles) = %d, want 1 (rejected add must not mutate)", len(pf.Profiles))
	}
}

func TestAddProfile_SecretCollision(t *testing.T) {
	var pf ProfilesFile
	must(t, pf.AddProfile(Profile{Name: "user_1", Secret: "shared"}))
	err := pf.AddProfile(Profile{Name: "user_2", Secret: "shared"})
	if err == nil {
		t.Fatal("AddProfile() with duplicate secret: expected error, got nil")
	}
	if len(pf.Profiles) != 1 {
		t.Errorf("len(Profiles) = %d, want 1", len(pf.Profiles))
	}
}

func TestRemoveProfile_Success(t *testing.T) {
	var pf ProfilesFile
	must(t, pf.AddProfile(Profile{Name: "user_1", Secret: "a"}))
	must(t, pf.AddProfile(Profile{Name: "user_2", Secret: "b"}))

	if err := pf.RemoveProfile("user_1"); err != nil {
		t.Fatalf("RemoveProfile() error = %v", err)
	}
	if len(pf.Profiles) != 1 || pf.Profiles[0].Name != "user_2" {
		t.Errorf("unexpected profiles after remove: %+v", pf.Profiles)
	}
}

func TestRemoveProfile_NotFound(t *testing.T) {
	var pf ProfilesFile
	must(t, pf.AddProfile(Profile{Name: "user_1", Secret: "a"}))

	err := pf.RemoveProfile("does-not-exist")
	if err == nil {
		t.Fatal("RemoveProfile() for missing name: expected error, got nil")
	}
	if len(pf.Profiles) != 1 {
		t.Errorf("len(Profiles) = %d, want 1 (failed remove must not mutate)", len(pf.Profiles))
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
