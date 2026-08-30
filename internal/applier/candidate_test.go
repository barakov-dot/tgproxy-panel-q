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
		t.Errorf("candidate written to %s, want under %s", path, filepath.Join(dir, candidateSubdir))
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
		t.Errorf("unexpected candidate content: %+v", got)
	}

	// No leftover temp files.
	entries, err := os.ReadDir(filepath.Join(dir, candidateSubdir))
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Errorf("leftover temp file: %s", e.Name())
		}
	}
}

func TestWriteCandidate_Rotation(t *testing.T) {
	dir := t.TempDir()
	pf := &ProfilesFile{}

	const keep = 3
	var paths []string
	for i := 0; i < keep+5; i++ {
		path, err := writeCandidate(dir, pf, keep)
		if err != nil {
			t.Fatalf("writeCandidate() iteration %d: error = %v", i, err)
		}
		paths = append(paths, path)
	}

	entries, err := os.ReadDir(filepath.Join(dir, candidateSubdir))
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	if len(entries) != keep {
		t.Fatalf("len(entries) = %d, want %d (rotation should prune old candidates)", len(entries), keep)
	}

	// The most recent file written must still exist.
	lastPath := paths[len(paths)-1]
	if _, err := os.Stat(lastPath); err != nil {
		t.Errorf("most recent candidate %s missing after rotation: %v", lastPath, err)
	}
}

func TestPruneOldCandidates_KeepZeroIsNoop(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(dir, candidatePrefix+"2026-01-01T00-00-00Z"+candidateSuffix)
	if err := os.WriteFile(f, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := pruneOldCandidates(dir, 0); err != nil {
		t.Fatalf("pruneOldCandidates() error = %v", err)
	}
	if _, err := os.Stat(f); err != nil {
		t.Errorf("file removed despite keep=0 being a no-op: %v", err)
	}
}
