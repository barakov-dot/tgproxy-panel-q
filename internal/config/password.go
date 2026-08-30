package config

import (
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

// HashPassword returns a bcrypt hash suitable for ADMIN_PASSWORD_HASH in .env.
// Used by the tgproxy-panel -hash-password CLI helper.
func HashPassword(password string) (string, error) {
	if password == "" {
		return "", fmt.Errorf("config: password must not be empty")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("config: hash password: %w", err)
	}
	return string(hash), nil
}
