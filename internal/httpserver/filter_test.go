package httpserver

import (
	"testing"
	"time"

	"github.com/barakov-dot/tgproxy-panel-q/internal/models"
)

func ptr[T any](v T) *T { return &v }

func testUsers() []*models.User {
	t1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	t3 := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	return []*models.User{
		{ID: 1, TelegramID: 300, Username: ptr("alice"), FirstName: ptr("Alice"), Status: models.StatusActive, RequestedAt: &t1},
		{ID: 2, TelegramID: 100, Username: ptr("bob"), LastName: ptr("Smith"), Status: models.StatusPending, RequestedAt: &t3},
		{ID: 3, TelegramID: 200, FirstName: ptr("Carol"), Status: models.StatusRevoked, RequestedAt: &t2},
	}
}

func TestFilterAndSortSearch(t *testing.T) {
	users := testUsers()

	got := FilterAndSort(users, "alice", SortRequestedAt, true)
	if len(got) != 1 || got[0].TelegramID != 300 {
		t.Fatalf("search 'alice' = %+v, want just telegram_id 300", got)
	}

	got = FilterAndSort(users, "smith", SortRequestedAt, true)
	if len(got) != 1 || got[0].TelegramID != 100 {
		t.Fatalf("search 'smith' (last name) = %+v", got)
	}

	got = FilterAndSort(users, "pending", SortRequestedAt, true)
	if len(got) != 1 || got[0].Status != models.StatusPending {
		t.Fatalf("search by status = %+v", got)
	}

	got = FilterAndSort(users, "200", SortRequestedAt, true)
	if len(got) != 1 || got[0].TelegramID != 200 {
		t.Fatalf("search by telegram_id substring = %+v", got)
	}

	got = FilterAndSort(users, "nonexistent", SortRequestedAt, true)
	if len(got) != 0 {
		t.Fatalf("search with no matches = %+v, want empty", got)
	}

	got = FilterAndSort(users, "", SortRequestedAt, true)
	if len(got) != 3 {
		t.Fatalf("empty search = %d results, want 3", len(got))
	}
}

func TestFilterAndSortByTelegramID(t *testing.T) {
	users := testUsers()

	asc := FilterAndSort(users, "", SortTelegramID, false)
	if asc[0].TelegramID != 100 || asc[1].TelegramID != 200 || asc[2].TelegramID != 300 {
		t.Fatalf("ascending telegram_id sort wrong: %v, %v, %v", asc[0].TelegramID, asc[1].TelegramID, asc[2].TelegramID)
	}

	desc := FilterAndSort(users, "", SortTelegramID, true)
	if desc[0].TelegramID != 300 || desc[2].TelegramID != 100 {
		t.Fatalf("descending telegram_id sort wrong: %v .. %v", desc[0].TelegramID, desc[2].TelegramID)
	}
}

func TestFilterAndSortByRequestedAt(t *testing.T) {
	users := testUsers()
	desc := FilterAndSort(users, "", SortRequestedAt, true)
	if desc[0].TelegramID != 100 { // t3 = latest
		t.Fatalf("newest-first requested_at sort wrong, got telegram_id=%d first", desc[0].TelegramID)
	}
}

func TestFilterAndSortNilTimesSortConsistently(t *testing.T) {
	users := []*models.User{
		{TelegramID: 1, IssuedAt: nil},
		{TelegramID: 2, IssuedAt: ptr(time.Now())},
	}
	// Must not panic, and nil should not be treated as "greatest".
	got := FilterAndSort(users, "", SortIssuedAt, true)
	if got[0].TelegramID != 2 {
		t.Errorf("descending issued_at: expected the set time first, got telegram_id=%d", got[0].TelegramID)
	}
}

func TestFilterPending(t *testing.T) {
	users := testUsers()
	got := FilterPending(users)
	if len(got) != 1 || got[0].Status != models.StatusPending {
		t.Fatalf("FilterPending = %+v", got)
	}
}

func TestParseSortColumnDefaultsToRequestedAt(t *testing.T) {
	if parseSortColumn("bogus") != SortRequestedAt {
		t.Error("unrecognized sort column should default to requested_at")
	}
	if parseSortColumn("status") != SortStatus {
		t.Error("valid sort column should round-trip")
	}
}

func TestParseSortDir(t *testing.T) {
	if parseSortDir("asc") != false {
		t.Error("'asc' should mean not-descending")
	}
	if parseSortDir("desc") != true {
		t.Error("'desc' should mean descending")
	}
	if parseSortDir("") != true {
		t.Error("missing dir should default to descending")
	}
}
