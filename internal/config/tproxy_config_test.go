package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadPublicHostname(t *testing.T) {
	dir := t.TempDir()
	path := writeTestTproxyConfig(t, dir, "t.neobyatnaya.net")

	got, err := ReadPublicHostname(path)
	if err != nil {
		t.Fatalf("ReadPublicHostname() error = %v", err)
	}
	if got != "t.neobyatnaya.net" {
		t.Errorf("ReadPublicHostname() = %q, want t.neobyatnaya.net", got)
	}
}

func TestReadPublicHostname_Missing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"listen":"127.0.0.1:8080"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadPublicHostname(path); err == nil {
		t.Fatal("expected error for missing public_hostname")
	}
}
