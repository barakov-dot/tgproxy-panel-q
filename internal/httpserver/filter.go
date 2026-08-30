package httpserver

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/barakov-dot/tgproxy-panel-q/internal/models"
)

// SortColumn identifies a user-list column sortable via ?sort=.
type SortColumn string

const (
	SortTelegramID  SortColumn = "telegram_id"
	SortName        SortColumn = "name"
	SortStatus      SortColumn = "status"
	SortRequestedAt SortColumn = "requested_at"
	SortIssuedAt    SortColumn = "issued_at"
)

// parseSortColumn maps a ?sort= query value to a SortColumn, defaulting to
// SortRequestedAt for anything unrecognized so a malformed/missing query
// param never errors, it just falls back to the list's natural order.
func parseSortColumn(v string) SortColumn {
	switch SortColumn(v) {
	case SortTelegramID, SortName, SortStatus, SortRequestedAt, SortIssuedAt:
		return SortColumn(v)
	default:
		return SortRequestedAt
	}
}

// parseSortDir maps a ?dir= query value to true for descending, defaulting
// to descending (newest/highest first) since that's the more useful default
// for both the requested_at and issued_at columns.
func parseSortDir(v string) bool {
	return v != "asc"
}

// FilterAndSort returns the subset of users whose telegram_id, username,
// first_name, last_name or status contains query (case-insensitive
// substring match), sorted by col. It never mutates users; the result is a
// new slice. At the ~50-user scale this project targets, a linear scan plus
// sort.Slice is simple and fast enough — see ListUsers's doc comment.
func FilterAndSort(users []*models.User, query string, col SortColumn, descending bool) []*models.User {
	out := filterUsers(users, query)
	sortUsers(out, col, descending)
	return out
}

// FilterPending returns only the users currently awaiting admin review —
// backs the user list's "Ожидают" tab (plan.md §5: pending requests shown
// "visually distinguished or on a separate tab").
func FilterPending(users []*models.User) []*models.User {
	out := make([]*models.User, 0, len(users))
	for _, u := range users {
		if u.IsPending() {
			out = append(out, u)
		}
	}
	return out
}

func filterUsers(users []*models.User, query string) []*models.User {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		out := make([]*models.User, len(users))
		copy(out, users)
		return out
	}

	out := make([]*models.User, 0, len(users))
	for _, u := range users {
		if userMatches(u, q) {
			out = append(out, u)
		}
	}
	return out
}

func userMatches(u *models.User, lowerQuery string) bool {
	fields := []string{
		strconv.FormatInt(u.TelegramID, 10),
		string(u.Status),
	}
	if u.Username != nil {
		fields = append(fields, *u.Username)
	}
	if u.FirstName != nil {
		fields = append(fields, *u.FirstName)
	}
	if u.LastName != nil {
		fields = append(fields, *u.LastName)
	}
	for _, f := range fields {
		if strings.Contains(strings.ToLower(f), lowerQuery) {
			return true
		}
	}
	return false
}

func sortUsers(users []*models.User, col SortColumn, descending bool) {
	less := func(i, j int) bool {
		a, b := users[i], users[j]
		switch col {
		case SortTelegramID:
			return a.TelegramID < b.TelegramID
		case SortName:
			return strings.ToLower(a.DisplayName()) < strings.ToLower(b.DisplayName())
		case SortStatus:
			if a.Status != b.Status {
				return a.Status < b.Status
			}
			return a.TelegramID < b.TelegramID
		case SortIssuedAt:
			return lessTimePtr(a.IssuedAt, b.IssuedAt)
		case SortRequestedAt:
			fallthrough
		default:
			return lessTimePtr(a.RequestedAt, b.RequestedAt)
		}
	}
	sort.SliceStable(users, func(i, j int) bool {
		if descending {
			return less(j, i)
		}
		return less(i, j)
	})
}

// lessTimePtr treats a nil time as older than any set time, so users
// without an issued_at (never issued) sort to one consistent end rather
// than panicking or comparing garbage.
func lessTimePtr(a, b *time.Time) bool {
	if a == nil && b == nil {
		return false
	}
	if a == nil {
		return true
	}
	if b == nil {
		return false
	}
	return a.Before(*b)
}
