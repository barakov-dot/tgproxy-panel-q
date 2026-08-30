package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type tproxyConfigFile struct {
	PublicHostname string `json:"public_hostname"`
}

// ReadPublicHostname returns public_hostname from tproxy-server's config.json.
func ReadPublicHostname(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("config: read tproxy config %s: %w", path, err)
	}
	var cfg tproxyConfigFile
	if err := json.Unmarshal(data, &cfg); err != nil {
		return "", fmt.Errorf("config: parse tproxy config %s: %w", path, err)
	}
	host := strings.ToLower(strings.TrimSpace(cfg.PublicHostname))
	if host == "" {
		return "", fmt.Errorf("config: public_hostname missing in %s", path)
	}
	return host, nil
}

// syncPublicHostname prefers public_hostname from tproxy-server config.json
// for proxy links. Capability derivation on the server uses that exact value.
func syncPublicHostname(cfg *Config) error {
	host, err := ReadPublicHostname(cfg.TproxyConfigPath)
	if err != nil {
		return err
	}
	cfg.TproxyHostname = host
	return nil
}
