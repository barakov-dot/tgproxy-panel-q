// Package secretgen produces profile secrets, profile names, and panel path tokens.
package secretgen

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math/big"
)

const pathTokenAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

// GenerateSecret returns a 32-character lowercase hex profile secret.
func GenerateSecret() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("secretgen: generate secret: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// ProfileName returns the profiles.json name for a Telegram user.
func ProfileName(telegramID int64) string {
	return fmt.Sprintf("user_%d", telegramID)
}

// GeneratePathToken returns a random 20-character [a-zA-Z0-9] token.
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
