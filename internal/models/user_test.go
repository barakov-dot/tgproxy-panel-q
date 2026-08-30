package models

import "testing"

func strPtr(s string) *string { return &s }

func TestUserStatus_Valid(t *testing.T) {
	for _, s := range []UserStatus{StatusPending, StatusActive, StatusRevoked, StatusDenied} {
		if !s.Valid() {
			t.Errorf("%q should be valid", s)
		}
	}
	if UserStatus("bogus").Valid() {
		t.Error("\"bogus\" should not be valid")
	}
}

func TestUser_IsActive(t *testing.T) {
	u := &User{Status: StatusActive}
	if !u.IsActive() {
		t.Error("expected IsActive() true for StatusActive")
	}
	u.Status = StatusRevoked
	if u.IsActive() {
		t.Error("expected IsActive() false for StatusRevoked")
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
