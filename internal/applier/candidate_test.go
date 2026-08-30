package applier

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteCandidate_ContentAndLocation(t *testing.T) {
	dir := t.TempDir()
	pf := &ProfilesFile{Profiles: []Profile{
		{Name: "user_1", Secret: "0123456789abcdef0123456789abcdef", Backend: "127.0.0.1:2398", CarrierMode: "https"},
	}}

	path, err := writeCandidate(dir, pf, 100)
	if err != nil {
		t.Fatalf("writeCandidate() error = %v", err)
	}
	if filepath.Dir(path) != filepath.Join(dir, candidateSubdir) {
		t.Errorf("candidate path = %s, want under candidates/", path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	var got ProfilesFile
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if len(got.Profiles) != 1 || got.Profiles[0].Name != "user_1" {
		t.Errorf("unexpected content: %+v", got)
	}
}

func TestWriteCandidate_Rotation(t *testing.T) {
	dir := t.TempDir()
	pf := &ProfilesFile{}
	const keep = 3
	for i := 0; i < keep+5; i++ {
		if _, err := writeCandidate(dir, pf, keep); err != nil {
			t.Fatalf("writeCandidate() iteration %d: %v", i, err)
		}
	}
	entries, err := os.ReadDir(filepath.Join(dir, candidateSubdir))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != keep {
		t.Errorf("len(candidates) = %d, want %d", len(entries), keep)
	}
}

func TestWriteCandidate_RejectsInvalid(t *testing.T) {
	pf := &ProfilesFile{Profiles: []Profile{
		{Name: "a", Secret: "x"},
		{Name: "a", Secret: "y"},
	}}
	_, err := writeCandidate(t.TempDir(), pf, 10)
	if err == nil {
		t.Fatal("writeCandidate() expected validation error")
	}
}
