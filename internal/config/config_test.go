package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func validEnv() map[string]string {
	return map[string]string{
		"PANEL_PORT":            "9000",
		"PANEL_PATH_TOKEN":      "abcDEF1234567890xyz",
		"ADMIN_LOGIN":           "admin",
		"ADMIN_PASSWORD_HASH":   "$2a$10$abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ01",
		"SESSION_SECRET":        "0123456789abcdef0123456789abcdef",
		"BOT_TOKEN":             "123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11",
		"ADMIN_TELEGRAM_ID":     "123456789",
		"AUTO_ISSUE":            "false",
		"TPROXY_HOSTNAME":       "proxy.example.com",
		"TPROXY_SERVICE_NAME":   "tproxy-server",
		"TPROXY_PROFILES_PATH":  "/etc/tproxy-server/profiles.json",
		"TPROXY_CONFIG_PATH":    "/etc/tproxy-server/config.json",
		"TPROXY_ADMIN_URL":      "http://127.0.0.1:8081",
		"CADDYFILE_PATH":        "/etc/caddy/Caddyfile",
		"TPROXY_BACKEND":        "127.0.0.1:2398",
		"TPROXY_CARRIER_MODE":   "https",
		"DB_PATH":               "/opt/tgproxy-panel/data/panel.db",
		"BACKUP_DIR":            "/opt/tgproxy-panel/backup",
		"BACKUP_KEEP":           "100",
		"APPLY_PROFILES_SCRIPT": "/opt/tgproxy-panel/bin/apply-profiles.sh",
		"LOG_FORMAT":            "json",
	}
}

func setEnv(t *testing.T, env map[string]string) {
	t.Helper()
	keys := []string{
		"ENV_FILE", "PANEL_PORT", "PANEL_PATH_TOKEN", "ADMIN_LOGIN", "ADMIN_PASSWORD_HASH",
		"SESSION_SECRET", "BOT_TOKEN", "ADMIN_TELEGRAM_ID", "AUTO_ISSUE",
		"TPROXY_HOSTNAME", "TPROXY_SERVICE_NAME", "TPROXY_PROFILES_PATH",
		"TPROXY_CONFIG_PATH", "TPROXY_ADMIN_URL", "CADDYFILE_PATH",
		"TPROXY_BACKEND", "TPROXY_CARRIER_MODE", "DB_PATH", "BACKUP_DIR",
		"BACKUP_KEEP", "APPLY_PROFILES_SCRIPT", "TPROXY_SERVER_BIN", "LOG_FORMAT",
	}
	for _, k := range keys {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
	for k, v := range env {
		t.Setenv(k, v)
	}
}

func TestLoad_Valid(t *testing.T) {
	setEnv(t, validEnv())

	cfg, err := loadFromEnv()
	if err != nil {
		t.Fatalf("loadFromEnv() error: %v", err)
	}
	if cfg.PanelPort != 9000 {
		t.Errorf("PanelPort = %d, want 9000", cfg.PanelPort)
	}
	if cfg.AdminTelegramID != 123456789 {
		t.Errorf("AdminTelegramID = %d, want 123456789", cfg.AdminTelegramID)
	}
	if cfg.AutoIssue {
		t.Errorf("AutoIssue = %v, want false", cfg.AutoIssue)
	}
	if cfg.LogFormat != "json" {
		t.Errorf("LogFormat = %q, want json", cfg.LogFormat)
	}
	if cfg.TproxyServerBin != defaultTproxyServerBin {
		t.Errorf("TproxyServerBin = %q, want default", cfg.TproxyServerBin)
	}
}

func TestLoadFromDotEnv(t *testing.T) {
	setEnv(t, map[string]string{})

	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	content := `# panel
PANEL_PORT=9000
PANEL_PATH_TOKEN=abcDEF1234567890xyz
ADMIN_LOGIN=admin
ADMIN_PASSWORD_HASH=$2a$10$abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ01
SESSION_SECRET=0123456789abcdef0123456789abcdef
BOT_TOKEN=123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11
ADMIN_TELEGRAM_ID=123456789
AUTO_ISSUE=true
TPROXY_HOSTNAME=proxy.example.com
TPROXY_SERVICE_NAME=tproxy-server
TPROXY_PROFILES_PATH=/etc/tproxy-server/profiles.json
TPROXY_CONFIG_PATH=/etc/tproxy-server/config.json
TPROXY_ADMIN_URL=http://127.0.0.1:8081
CADDYFILE_PATH=/etc/caddy/Caddyfile
TPROXY_BACKEND=127.0.0.1:2398
TPROXY_CARRIER_MODE=https
DB_PATH=/opt/tgproxy-panel/data/panel.db
BACKUP_DIR=/opt/tgproxy-panel/backup
BACKUP_KEEP=100
APPLY_PROFILES_SCRIPT=/opt/tgproxy-panel/bin/apply-profiles.sh
LOG_FORMAT=text
`
	if err := os.WriteFile(envPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFile(envPath)
	if err != nil {
		t.Fatalf("LoadFile() error: %v", err)
	}
	if !cfg.AutoIssue {
		t.Error("AutoIssue = false, want true from .env")
	}
	if cfg.LogFormat != "text" {
		t.Errorf("LogFormat = %q, want text", cfg.LogFormat)
	}
}

func TestLoadDotEnv_ProcessEnvOverridesFile(t *testing.T) {
	setEnv(t, map[string]string{"PANEL_PORT": "8080"})

	dir := t.TempDir()
	envPath := filepath.Join(dir, "test.env")
	if err := os.WriteFile(envPath, []byte("PANEL_PORT=9000\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := loadDotEnv(envPath); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("PANEL_PORT"); got != "8080" {
		t.Errorf("PANEL_PORT = %q, want 8080 (process env should override file)", got)
	}
}

func TestLoad_MissingRequiredFields(t *testing.T) {
	setEnv(t, map[string]string{})

	_, err := loadFromEnv()
	if err == nil {
		t.Fatal("expected error for empty environment")
	}
	for _, want := range []string{"PANEL_PORT", "BOT_TOKEN", "ADMIN_TELEGRAM_ID", "TPROXY_HOSTNAME"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %s", err.Error(), want)
		}
	}
}

func TestLoad_InvalidFields(t *testing.T) {
	cases := []struct {
		name    string
		key     string
		value   string
		wantSub string
	}{
		{"invalid port", "PANEL_PORT", "not-a-port", "PANEL_PORT"},
		{"port out of range", "PANEL_PORT", "70000", "PANEL_PORT"},
		{"invalid bcrypt", "ADMIN_PASSWORD_HASH", "plaintext", "ADMIN_PASSWORD_HASH"},
		{"invalid telegram id", "ADMIN_TELEGRAM_ID", "nope", "ADMIN_TELEGRAM_ID"},
		{"invalid backend", "TPROXY_BACKEND", "bad", "TPROXY_BACKEND"},
		{"relative path", "DB_PATH", "relative/db", "DB_PATH"},
		{"invalid log format", "LOG_FORMAT", "xml", "LOG_FORMAT"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := validEnv()
			env[tc.key] = tc.value
			setEnv(t, env)
			_, err := loadFromEnv()
			if err == nil || !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("expected error mentioning %q, got %v", tc.wantSub, err)
			}
		})
	}
}

func TestHashPassword(t *testing.T) {
	hash, err := HashPassword("secret-pass")
	if err != nil {
		t.Fatalf("HashPassword() error: %v", err)
	}
	if !strings.HasPrefix(hash, "$2") {
		t.Errorf("hash %q does not look like bcrypt", hash)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte("secret-pass")); err != nil {
		t.Errorf("bcrypt compare failed: %v", err)
	}
}

func TestHashPassword_EmptyRejected(t *testing.T) {
	if _, err := HashPassword(""); err == nil {
		t.Fatal("expected error for empty password")
	}
}

func TestLoadDotEnv_InvalidLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.env")
	if err := os.WriteFile(path, []byte("NOTVALID\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := loadDotEnv(path); err == nil || !strings.Contains(err.Error(), "invalid line") {
		t.Fatalf("expected invalid line error, got %v", err)
	}
}
