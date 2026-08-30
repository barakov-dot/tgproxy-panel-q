// Package config loads tgproxy-panel's configuration from environment
// variables (see .env.example and plan.md §9). The process environment is
// assumed to already be populated (systemd EnvironmentFile= or a wrapper
// script) — this package intentionally does not parse .env files itself.
package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Config holds every setting tgproxy-panel needs, typed and validated.
type Config struct {
	PanelPort      int
	PanelPathToken string

	AdminLogin        string
	AdminPasswordHash string
	SessionSecret     string

	BotToken        string
	AdminTelegramID int64
	AutoIssue       bool

	TproxyHostname     string
	TproxyServiceName  string
	TproxyProfilesPath string
	TproxyConfigPath   string
	TproxyAdminURL     string
	CaddyfilePath      string
	TproxyBackend      string
	TproxyCarrierMode  string

	DBPath     string
	BackupDir  string
	BackupKeep int

	ApplyProfilesScript string

	// TproxyServerBin is the tproxy-server binary internal/applier shells
	// out to for `-check` validation before staging a candidate
	// profiles.json (see CLAUDE.md's verified facts on -check). Optional:
	// defaults to where tproxy-server's own install.sh installs it, since
	// most deployments never need to override this.
	TproxyServerBin string

	LogFormat string
}

// defaultTproxyServerBin is where tproxy-server's own install.sh installs
// the binary (verified against the reference install.sh); used when
// TPROXY_SERVER_BIN is unset.
const defaultTproxyServerBin = "/usr/local/bin/tproxy-server"

// Load reads and validates configuration from the process environment. It
// returns a descriptive error (rather than panicking) listing every problem
// found, so a misconfigured deployment fails fast and clearly at startup.
func Load() (*Config, error) {
	var errs []error

	cfg := &Config{}

	cfg.PanelPort = requirePort(&errs, "PANEL_PORT")
	cfg.PanelPathToken = requireMinLen(&errs, "PANEL_PATH_TOKEN", 8)

	cfg.AdminLogin = require(&errs, "ADMIN_LOGIN")
	cfg.AdminPasswordHash = requireBcryptHash(&errs, "ADMIN_PASSWORD_HASH")
	cfg.SessionSecret = requireMinLen(&errs, "SESSION_SECRET", 16)

	cfg.BotToken = require(&errs, "BOT_TOKEN")
	cfg.AdminTelegramID = requirePositiveInt64(&errs, "ADMIN_TELEGRAM_ID")
	cfg.AutoIssue = requireBool(&errs, "AUTO_ISSUE")

	cfg.TproxyHostname = require(&errs, "TPROXY_HOSTNAME")
	cfg.TproxyServiceName = require(&errs, "TPROXY_SERVICE_NAME")
	cfg.TproxyProfilesPath = requireAbsPath(&errs, "TPROXY_PROFILES_PATH")
	cfg.TproxyConfigPath = requireAbsPath(&errs, "TPROXY_CONFIG_PATH")
	cfg.TproxyAdminURL = requireURL(&errs, "TPROXY_ADMIN_URL")
	cfg.CaddyfilePath = requireAbsPath(&errs, "CADDYFILE_PATH")
	cfg.TproxyBackend = requireHostPort(&errs, "TPROXY_BACKEND")
	cfg.TproxyCarrierMode = require(&errs, "TPROXY_CARRIER_MODE")

	cfg.DBPath = requireAbsPath(&errs, "DB_PATH")
	cfg.BackupDir = requireAbsPath(&errs, "BACKUP_DIR")
	cfg.BackupKeep = requirePositiveInt(&errs, "BACKUP_KEEP")

	cfg.ApplyProfilesScript = requireAbsPath(&errs, "APPLY_PROFILES_SCRIPT")

	cfg.TproxyServerBin = optionalAbsPath(&errs, "TPROXY_SERVER_BIN", defaultTproxyServerBin)

	cfg.LogFormat = logFormat(&errs)

	if len(errs) > 0 {
		return nil, fmt.Errorf("config: %w", errors.Join(errs...))
	}
	return cfg, nil
}

func getenv(key string) string {
	return strings.TrimSpace(os.Getenv(key))
}

func require(errs *[]error, key string) string {
	v := getenv(key)
	if v == "" {
		*errs = append(*errs, fmt.Errorf("%s is required", key))
	}
	return v
}

func requireMinLen(errs *[]error, key string, minLen int) string {
	v := require(errs, key)
	if v != "" && len(v) < minLen {
		*errs = append(*errs, fmt.Errorf("%s must be at least %d characters", key, minLen))
	}
	return v
}

func requireAbsPath(errs *[]error, key string) string {
	v := require(errs, key)
	if v != "" && !filepath.IsAbs(v) {
		*errs = append(*errs, fmt.Errorf("%s must be an absolute path, got %q", key, v))
	}
	return v
}

func requireBcryptHash(errs *[]error, key string) string {
	v := require(errs, key)
	if v != "" && !strings.HasPrefix(v, "$2") {
		*errs = append(*errs, fmt.Errorf("%s does not look like a bcrypt hash (expected a $2.. prefix, generate with tgproxy-panel -hash-password)", key))
	}
	return v
}

func requireURL(errs *[]error, key string) string {
	v := require(errs, key)
	if v == "" {
		return v
	}
	u, err := url.Parse(v)
	if err != nil || u.Scheme == "" || u.Host == "" {
		*errs = append(*errs, fmt.Errorf("%s must be a valid absolute URL, got %q", key, v))
	}
	return v
}

func requireHostPort(errs *[]error, key string) string {
	v := require(errs, key)
	if v == "" {
		return v
	}
	if _, _, err := net.SplitHostPort(v); err != nil {
		*errs = append(*errs, fmt.Errorf("%s must be a host:port pair, got %q: %w", key, v, err))
	}
	return v
}

func requirePort(errs *[]error, key string) int {
	v := require(errs, key)
	if v == "" {
		return 0
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 || n > 65535 {
		*errs = append(*errs, fmt.Errorf("%s must be a valid TCP port (1-65535), got %q", key, v))
		return 0
	}
	return n
}

func requirePositiveInt(errs *[]error, key string) int {
	v := require(errs, key)
	if v == "" {
		return 0
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		*errs = append(*errs, fmt.Errorf("%s must be a positive integer, got %q", key, v))
		return 0
	}
	return n
}

func requirePositiveInt64(errs *[]error, key string) int64 {
	v := require(errs, key)
	if v == "" {
		return 0
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n <= 0 {
		*errs = append(*errs, fmt.Errorf("%s must be a positive integer, got %q", key, v))
		return 0
	}
	return n
}

func requireBool(errs *[]error, key string) bool {
	v := require(errs, key)
	if v == "" {
		return false
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		*errs = append(*errs, fmt.Errorf("%s must be a boolean (true/false), got %q", key, v))
		return false
	}
	return b
}

// optionalAbsPath reads an optional absolute-path variable, falling back to
// def when unset (mirroring logFormat's default-handling below).
func optionalAbsPath(errs *[]error, key, def string) string {
	v := getenv(key)
	if v == "" {
		return def
	}
	if !filepath.IsAbs(v) {
		*errs = append(*errs, fmt.Errorf("%s must be an absolute path, got %q", key, v))
	}
	return v
}

// logFormat is the only variable with a real default (matching the "json is
// the default" comment in .env.example), so it does not go through require.
func logFormat(errs *[]error) string {
	v := getenv("LOG_FORMAT")
	if v == "" {
		return "json"
	}
	if v != "json" && v != "text" {
		*errs = append(*errs, fmt.Errorf("LOG_FORMAT must be \"json\" or \"text\", got %q", v))
	}
	return v
}
