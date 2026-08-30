package config

import (
	"os"
	"strings"
	"testing"
)

// validEnv returns a fully-populated, valid environment matching
// .env.example, so individual tests can override just the variable they
// care about.
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

// setEnv clears every key Load() reads, then applies env, so tests never
// leak into or depend on the real process environment.
func setEnv(t *testing.T, env map[string]string) {
	t.Helper()
	keys := []string{
		"PANEL_PORT", "PANEL_PATH_TOKEN", "ADMIN_LOGIN", "ADMIN_PASSWORD_HASH",
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

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error for valid env: %v", err)
	}
	if cfg.PanelPort != 9000 {
		t.Errorf("PanelPort = %d, want 9000", cfg.PanelPort)
	}
	if cfg.AdminTelegramID != 123456789 {
		t.Errorf("AdminTelegramID = %d, want 123456789", cfg.AdminTelegramID)
	}
	if cfg.AutoIssue != false {
		t.Errorf("AutoIssue = %v, want false", cfg.AutoIssue)
	}
	if cfg.LogFormat != "json" {
		t.Errorf("LogFormat = %q, want json", cfg.LogFormat)
	}
	if cfg.BackupKeep != 100 {
		t.Errorf("BackupKeep = %d, want 100", cfg.BackupKeep)
	}
	if cfg.TproxyServerBin != "/usr/local/bin/tproxy-server" {
		t.Errorf("TproxyServerBin = %q, want default /usr/local/bin/tproxy-server", cfg.TproxyServerBin)
	}
}

func TestLoad_TproxyServerBinOverride(t *testing.T) {
	env := validEnv()
	env["TPROXY_SERVER_BIN"] = "/opt/tproxy-server/bin/tproxy-server"
	setEnv(t, env)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if cfg.TproxyServerBin != "/opt/tproxy-server/bin/tproxy-server" {
		t.Errorf("TproxyServerBin = %q, want override", cfg.TproxyServerBin)
	}
}

func TestLoad_TproxyServerBinRelativeRejected(t *testing.T) {
	env := validEnv()
	env["TPROXY_SERVER_BIN"] = "relative/tproxy-server"
	setEnv(t, env)

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "TPROXY_SERVER_BIN") {
		t.Fatalf("expected TPROXY_SERVER_BIN error, got %v", err)
	}
}

func TestLoad_LogFormatDefaultsToJSON(t *testing.T) {
	env := validEnv()
	delete(env, "LOG_FORMAT")
	setEnv(t, env)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if cfg.LogFormat != "json" {
		t.Errorf("LogFormat = %q, want default json", cfg.LogFormat)
	}
}

func TestLoad_MissingRequiredFields(t *testing.T) {
	setEnv(t, map[string]string{})

	_, err := Load()
	if err == nil {
		t.Fatal("Load() with empty environment should error")
	}
	// Spot-check that a handful of distinct problems are all surfaced, not
	// just the first one encountered.
	for _, want := range []string{"PANEL_PORT", "BOT_TOKEN", "ADMIN_TELEGRAM_ID", "TPROXY_HOSTNAME"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention missing field %s", err.Error(), want)
		}
	}
}

func TestLoad_InvalidPort(t *testing.T) {
	env := validEnv()
	env["PANEL_PORT"] = "not-a-port"
	setEnv(t, env)

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "PANEL_PORT") {
		t.Fatalf("expected PANEL_PORT error, got %v", err)
	}
}

func TestLoad_PortOutOfRange(t *testing.T) {
	env := validEnv()
	env["PANEL_PORT"] = "70000"
	setEnv(t, env)

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "PANEL_PORT") {
		t.Fatalf("expected PANEL_PORT range error, got %v", err)
	}
}

func TestLoad_InvalidBcryptHash(t *testing.T) {
	env := validEnv()
	env["ADMIN_PASSWORD_HASH"] = "plaintext-not-a-hash"
	setEnv(t, env)

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "ADMIN_PASSWORD_HASH") {
		t.Fatalf("expected ADMIN_PASSWORD_HASH error, got %v", err)
	}
}

func TestLoad_InvalidTelegramID(t *testing.T) {
	env := validEnv()
	env["ADMIN_TELEGRAM_ID"] = "not-a-number"
	setEnv(t, env)

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "ADMIN_TELEGRAM_ID") {
		t.Fatalf("expected ADMIN_TELEGRAM_ID error, got %v", err)
	}
}

func TestLoad_InvalidBackend(t *testing.T) {
	env := validEnv()
	env["TPROXY_BACKEND"] = "not-a-host-port"
	setEnv(t, env)

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "TPROXY_BACKEND") {
		t.Fatalf("expected TPROXY_BACKEND error, got %v", err)
	}
}

func TestLoad_RelativePathRejected(t *testing.T) {
	env := validEnv()
	env["DB_PATH"] = "relative/path/panel.db"
	setEnv(t, env)

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "DB_PATH") {
		t.Fatalf("expected DB_PATH error, got %v", err)
	}
}

func TestLoad_InvalidLogFormat(t *testing.T) {
	env := validEnv()
	env["LOG_FORMAT"] = "xml"
	setEnv(t, env)

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "LOG_FORMAT") {
		t.Fatalf("expected LOG_FORMAT error, got %v", err)
	}
}
