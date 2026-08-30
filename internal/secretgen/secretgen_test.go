package secretgen

import (
	"regexp"
	"testing"
)

var hex32 = regexp.MustCompile(`^[0-9a-f]{32}$`)

func TestGenerateSecret_FormatAndLength(t *testing.T) {
	s, err := GenerateSecret()
	if err != nil {
		t.Fatalf("GenerateSecret() error = %v", err)
	}
	if len(s) != 32 {
		t.Errorf("len(secret) = %d, want 32", len(s))
	}
	if !hex32.MatchString(s) {
		t.Errorf("secret %q does not match 32 lowercase hex chars", s)
	}
}

func TestGenerateSecret_Randomness(t *testing.T) {
	a, err := GenerateSecret()
	if err != nil {
		t.Fatalf("GenerateSecret() error = %v", err)
	}
	b, err := GenerateSecret()
	if err != nil {
		t.Fatalf("GenerateSecret() error = %v", err)
	}
	if a == b {
		t.Errorf("two consecutive GenerateSecret() calls produced the same value: %q", a)
	}
}

func TestGenerateSecret_ManyUnique(t *testing.T) {
	seen := make(map[string]bool)
	const n = 1000
	for i := 0; i < n; i++ {
		s, err := GenerateSecret()
		if err != nil {
			t.Fatalf("GenerateSecret() error = %v", err)
		}
		if seen[s] {
			t.Fatalf("duplicate secret generated after %d calls: %q", i, s)
		}
		seen[s] = true
	}
}

func TestProfileName(t *testing.T) {
	cases := []struct {
		telegramID int64
		want       string
	}{
		{123456789, "user_123456789"},
		{1, "user_1"},
		{0, "user_0"},
	}
	for _, tc := range cases {
		if got := ProfileName(tc.telegramID); got != tc.want {
			t.Errorf("ProfileName(%d) = %q, want %q", tc.telegramID, got, tc.want)
		}
	}
}

func TestProfileName_Deterministic(t *testing.T) {
	a := ProfileName(42)
	b := ProfileName(42)
	if a != b {
		t.Errorf("ProfileName(42) not deterministic: %q vs %q", a, b)
	}
}

var pathTokenRe = regexp.MustCompile(`^[a-zA-Z0-9]{20}$`)

func TestGeneratePathToken_FormatAndLength(t *testing.T) {
	tok, err := GeneratePathToken()
	if err != nil {
		t.Fatalf("GeneratePathToken() error = %v", err)
	}
	if len(tok) != 20 {
		t.Errorf("len(token) = %d, want 20", len(tok))
	}
	if !pathTokenRe.MatchString(tok) {
		t.Errorf("token %q does not match [a-zA-Z0-9]{20}", tok)
	}
}

func TestGeneratePathToken_Randomness(t *testing.T) {
	a, err := GeneratePathToken()
	if err != nil {
		t.Fatalf("GeneratePathToken() error = %v", err)
	}
	b, err := GeneratePathToken()
	if err != nil {
		t.Fatalf("GeneratePathToken() error = %v", err)
	}
	if a == b {
		t.Errorf("two consecutive GeneratePathToken() calls produced the same value: %q", a)
	}
}
