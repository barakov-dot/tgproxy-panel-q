// Package config loads tgproxy-panel configuration from a .env file and the
// process environment (see .env.example and PLAN.md §9). Values already present
// in the process environment override .env entries.
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
	TproxyServerBin     string

	LogFormat string
}

const (
	defaultEnvFile          = ".env"
	defaultTproxyServerBin  = "/usr/local/bin/tproxy-server"
	minPanelPathTokenLength = 8
	minSessionSecretLength  = 16
)

// Load reads .env (if present) then validates configuration from the process
// environment. Use ENV_FILE to point at a non-default .env path.
func Load() (*Config, error) {
	envFile := getenv("ENV_FILE")
	if envFile == "" {
		envFile = defaultEnvFile
	}
	if err := loadDotEnv(envFile); err != nil {
		return nil, err
	}
	return loadFromEnv()
}

// LoadFile loads configuration after applying variables from the given .env
// file. Process environment still overrides file values.
func LoadFile(path string) (*Config, error) {
	if err := loadDotEnv(path); err != nil {
		return nil, err
	}
	return loadFromEnv()
}

func loadFromEnv() (*Config, error) {
	var errs []error

	cfg := &Config{}

	cfg.PanelPort = requirePort(&errs, "PANEL_PORT")
	cfg.PanelPathToken = requireMinLen(&errs, "PANEL_PATH_TOKEN", minPanelPathTokenLength)

	cfg.AdminLogin = require(&errs, "ADMIN_LOGIN")
	cfg.AdminPasswordHash = requireBcryptHash(&errs, "ADMIN_PASSWORD_HASH")
	cfg.SessionSecret = requireMinLen(&errs, "SESSION_SECRET", minSessionSecretLength)

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
	if err := syncPublicHostname(cfg); err != nil {
		return nil, err
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
		*errs = append(*errs, fmt.Errorf("%s does not look like a bcrypt hash (expected a $2.. prefix; generate with tgproxy-panel -hash-password)", key))
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
