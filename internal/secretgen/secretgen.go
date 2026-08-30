// Package secretgen produces the three random/derived strings tgproxy-panel
// hands out: a profile secret, a profile name, and the panel's own
// URL path token. Pure functions, no I/O beyond crypto/rand.
package secretgen

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math/big"
)

// pathTokenAlphabet matches the [a-zA-Z0-9] charset plan.md §8 step 6 and
// .env.example's PANEL_PATH_TOKEN comment specify.
const pathTokenAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

// GenerateSecret returns a new 32-lowercase-hex-char profile secret: 16
// random bytes, hex-encoded, matching profiles.example.json's `secret`
// format exactly (see CLAUDE.md's verified facts).
func GenerateSecret() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("secretgen: generate secret: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// ProfileName returns the profiles.json `name` for a Telegram user, per
// plan.md §4's `user_<telegram_id>` convention. It is deterministic (no
// randomness needed): telegram_id is unique per Telegram account and the
// users.telegram_id column is UNIQUE, so distinct users can never produce
// colliding profile names.
func ProfileName(telegramID int64) string {
	return fmt.Sprintf("user_%d", telegramID)
}

// GeneratePathToken returns a new random 20-character token drawn from
// [a-zA-Z0-9], for the panel's secret URL path segment (plan.md §8 step 6).
// It samples via crypto/rand.Int rather than byte-mod-62 to avoid modulo
// bias in the alphabet selection.
func GeneratePathToken() (string, error) {
	const length = 20
	alphabetSize := big.NewInt(int64(len(pathTokenAlphabet)))

	out := make([]byte, length)
	for i := range out {
		n, err := rand.Int(rand.Reader, alphabetSize)
		if err != nil {
			return "", fmt.Errorf("secretgen: generate path token: %w", err)
		}
		out[i] = pathTokenAlphabet[n.Int64()]
	}
	return string(out), nil
}
