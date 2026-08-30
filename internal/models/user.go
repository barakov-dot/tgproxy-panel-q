// Package models holds shared data types for store, service, httpserver, and bot.
package models

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// UserStatus is the lifecycle state of a user's proxy access request.
type UserStatus string

const (
	StatusPending UserStatus = "pending"
	StatusActive  UserStatus = "active"
	StatusRevoked UserStatus = "revoked"
	StatusDenied  UserStatus = "denied"
)

// Valid reports whether s is one of the known statuses.
func (s UserStatus) Valid() bool {
	switch s {
	case StatusPending, StatusActive, StatusRevoked, StatusDenied:
		return true
	}
	return false
}

// User mirrors the users table. Nullable columns use pointers.
type User struct {
	ID          int64
	TelegramID  int64
	Username    *string
	FirstName   *string
	LastName    *string
	Status      UserStatus
	ProfileName *string
	Secret      *string
	RequestedAt *time.Time
	IssuedAt    *time.Time
	RevokedAt   *time.Time
}

func (u *User) IsActive() bool  { return u.Status == StatusActive }
func (u *User) IsPending() bool { return u.Status == StatusPending }

// DisplayName returns "First Last", then "@username", then the Telegram ID.
func (u *User) DisplayName() string {
	first, last := "", ""
	if u.FirstName != nil {
		first = *u.FirstName
	}
	if u.LastName != nil {
		last = *u.LastName
	}
	name := first
	if last != "" {
		if name != "" {
			name += " "
		}
		name += last
	}
	if name != "" {
		return name
	}
	if u.Username != nil && *u.Username != "" {
		return "@" + *u.Username
	}
	return strconv.FormatInt(u.TelegramID, 10)
}

// ProxyLink builds the t.me/webproxy deep link when hostname and secret are set.
// Hostname must match config.json public_hostname exactly (lowercase ASCII).
func (u *User) ProxyLink(hostname string) string {
	if hostname == "" || u.Secret == nil || *u.Secret == "" {
		return ""
	}
	host := strings.ToLower(strings.TrimSpace(hostname))
	secret := strings.ToLower(strings.TrimSpace(*u.Secret))
	return fmt.Sprintf("https://t.me/webproxy?server=%s&secret=%s", host, secret)
}

// HasProfile reports whether profile_name and secret are both set.
func (u *User) HasProfile() bool {
	return u.ProfileName != nil && *u.ProfileName != "" &&
		u.Secret != nil && *u.Secret != ""
}
