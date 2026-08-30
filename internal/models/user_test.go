package models

import "testing"

func strPtr(s string) *string { return &s }

func TestUserStatus_Valid(t *testing.T) {
	valid := []UserStatus{StatusPending, StatusActive, StatusRevoked, StatusDenied}
	for _, s := range valid {
		if !s.Valid() {
			t.Errorf("%q should be valid", s)
		}
	}
	if UserStatus("bogus").Valid() {
		t.Error("bogus should not be valid")
	}
}

func TestUser_IsActive(t *testing.T) {
	u := &User{Status: StatusActive}
	if !u.IsActive() {
		t.Error("expected active")
	}
	u.Status = StatusRevoked
	if u.IsActive() {
		t.Error("expected not active after revoke")
	}
}

func TestUser_DisplayName(t *testing.T) {
	cases := []struct {
		name string
		user User
		want string
	}{
		{"full name", User{FirstName: strPtr("Ada"), LastName: strPtr("Lovelace")}, "Ada Lovelace"},
		{"first only", User{FirstName: strPtr("Ada")}, "Ada"},
		{"username fallback", User{Username: strPtr("adal")}, "@adal"},
		{"telegram id fallback", User{TelegramID: 42}, "42"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.user.DisplayName(); got != c.want {
				t.Errorf("DisplayName() = %q, want %q", got, c.want)
			}
		})
	}
}

func TestUser_ProxyLink(t *testing.T) {
	secret := "0123456789abcdef0123456789abcdef"
	u := &User{Secret: &secret}

	if got := u.ProxyLink(""); got != "" {
		t.Errorf("empty hostname: got %q", got)
	}
	if got := u.ProxyLink("proxy.example.com"); got == "" {
		t.Fatal("expected non-empty link")
	}
	if u.ProxyLink("proxy.example.com") != "https://t.me/webproxy?server=proxy.example.com&secret=0123456789abcdef0123456789abcdef" {
		t.Error("unexpected proxy link format")
	}
}

func TestUser_HasProfile(t *testing.T) {
	name := "user_1"
	secret := "abc"
	u := &User{ProfileName: &name, Secret: &secret}
	if !u.HasProfile() {
		t.Error("expected HasProfile true")
	}
	u.Secret = nil
	if u.HasProfile() {
		t.Error("expected HasProfile false without secret")
	}
}

func TestAutoIssueEnabled(t *testing.T) {
	cases := []struct {
		value string
		want  bool
	}{
		{"true", true},
		{"false", false},
		{"", false},
	}
	for _, c := range cases {
		if got := AutoIssueEnabled(c.value); got != c.want {
			t.Errorf("AutoIssueEnabled(%q) = %v, want %v", c.value, got, c.want)
		}
	}
}
