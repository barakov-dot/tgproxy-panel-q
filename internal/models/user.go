// Package models holds the plain data types shared by internal/store,
// internal/httpserver, internal/bot and internal/service. It has no
// dependencies on any other internal package.
package models

import (
	"strconv"
	"time"
)

// UserStatus is the lifecycle state of a user's proxy access request, per
// plan.md §4's CHECK(status IN (...)) constraint.
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

// User mirrors the `users` table in plan.md §4. Username, FirstName,
// LastName, ProfileName and Secret are nullable columns and use pointers so
// "not set" is distinguishable from an empty string.
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

// IsActive reports whether the user currently has a live proxy profile.
func (u *User) IsActive() bool {
	return u.Status == StatusActive
}

// IsPending reports whether the user has a request awaiting admin review.
func (u *User) IsPending() bool {
	return u.Status == StatusPending
}

// DisplayName returns the best available human-readable name for the user:
// "First Last", falling back to "@username", falling back to the numeric
// Telegram ID.
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
