package httpserver

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSettingsPageShowsAutoIssueAndBackups(t *testing.T) {
	ts := newTestServer(t)
	backupFile := filepath.Join(ts.cfg.BackupDir, "profiles.json.2026-08-30T12-00-00Z.bak")
	if err := os.WriteFile(backupFile, []byte("{}"), 0o600); err != nil {
		t.Fatalf("write backup fixture: %v", err)
	}

	rr := ts.authedRequest(t, http.MethodGet, ts.base()+"/settings", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if !strings.Contains(body, "profiles.json.2026-08-30T12-00-00Z.bak") {
		t.Errorf("expected backup filename in body:\n%s", body)
	}
}

func TestSetAutoIssueTogglesSetting(t *testing.T) {
	ts := newTestServer(t)

	rr := ts.authedRequest(t, http.MethodPost, ts.base()+"/settings/auto-issue", strings.NewReader("auto_issue=on"))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rr.Code, rr.Body.String())
	}

	got, ok := ts.store.settings["auto_issue"]
	if !ok || got != "true" {
		t.Errorf("auto_issue setting = %q, ok=%v, want true", got, ok)
	}

	// Unchecking: the browser omits the field entirely.
	rr = ts.authedRequest(t, http.MethodPost, ts.base()+"/settings/auto-issue", strings.NewReader(""))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	got, ok = ts.store.settings["auto_issue"]
	if !ok || got != "false" {
		t.Errorf("auto_issue setting after uncheck = %q, ok=%v, want false", got, ok)
	}
}

func TestListBackupsSortedNewestFirst(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "old.bak")
	newer := filepath.Join(dir, "newer.bak")
	if err := os.WriteFile(old, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newer, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(time.Hour)
	if err := os.Chtimes(newer, future, future); err != nil {
		t.Fatal(err)
	}

	got, err := listBackups(dir)
	if err != nil {
		t.Fatalf("listBackups: %v", err)
	}
	if len(got) != 2 || got[0].Name != "newer.bak" {
		t.Fatalf("listBackups = %+v, want newer.bak first", got)
	}
}
