package auth

import (
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestCheckLogin(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("correct horse battery staple"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("bcrypt.GenerateFromPassword: %v", err)
	}

	const wantLogin = "admin"

	cases := []struct {
		name     string
		login    string
		password string
		want     bool
	}{
		{"correct login and password", wantLogin, "correct horse battery staple", true},
		{"wrong password", wantLogin, "wrong password", false},
		{"wrong login", "notadmin", "correct horse battery staple", false},
		{"wrong login and password", "notadmin", "wrong password", false},
		{"empty password", wantLogin, "", false},
		{"empty login", "", "correct horse battery staple", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := CheckLogin(wantLogin, string(hash), tc.login, tc.password)
			if got != tc.want {
				t.Errorf("CheckLogin(%q, hash, %q, %q) = %v, want %v", wantLogin, tc.login, tc.password, got, tc.want)
			}
		})
	}
}

func TestConstantTimeStringsEqual(t *testing.T) {
	cases := []struct {
		name string
		a, b string
		want bool
	}{
		{"equal strings", "admin", "admin", true},
		{"different length", "admin", "adminn", false},
		{"non-empty vs empty", "admin", "", false},
		{"both empty", "", "", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := constantTimeStringsEqual(tc.a, tc.b)
			if got != tc.want {
				t.Errorf("constantTimeStringsEqual(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}
