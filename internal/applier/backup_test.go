package applier

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBackupProfiles_Rotation(t *testing.T) {
	dir := t.TempDir()
	profilesPath := filepath.Join(dir, "profiles.json")
	backupDir := filepath.Join(dir, "backup")

	initial := `{"profiles":[{"name":"old","secret":"000102030405060708090a0b0c0d0e0f","backend":"127.0.0.1:2398","carrier_mode":"https"}]}`
	if err := os.WriteFile(profilesPath, []byte(initial), 0o600); err != nil {
		t.Fatal(err)
	}

	const keep = 3
	for i := 0; i < keep+2; i++ {
		content := initial
		if i > 0 {
			content = `{"profiles":[]}`
		}
		if err := os.WriteFile(profilesPath, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := backupProfiles(profilesPath, backupDir, keep); err != nil {
			t.Fatalf("backupProfiles() iteration %d: %v", i, err)
		}
	}

	entries, err := os.ReadDir(backupDir)
	if err != nil {
		t.Fatal(err)
	}
	var backups []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), backupPrefix) {
			backups = append(backups, e.Name())
		}
	}
	if len(backups) != keep {
		t.Errorf("len(backups) = %d, want %d", len(backups), keep)
	}
}

func TestLatestBackup(t *testing.T) {
	dir := t.TempDir()
	names := []string{
		"profiles.json.2026-01-01T00-00-00Z.bak",
		"profiles.json.2026-01-02T00-00-00Z.bak",
	}
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	got, err := latestBackup(dir)
	if err != nil {
		t.Fatalf("latestBackup() error = %v", err)
	}
	want := filepath.Join(dir, names[1])
	if got != want {
		t.Errorf("latestBackup() = %q, want %q", got, want)
	}
}