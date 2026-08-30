package applier

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"
)

const (
	backupPrefix = "profiles.json."
	backupSuffix = ".bak"
)

// backupTimeFormat matches plan.md §10 example filenames (filesystem-safe UTC).
const backupTimeFormat = "2006-01-02T15-04-05.000000000Z"

var backupSeq atomic.Uint64

// backupProfiles copies the current profiles file into backupDir and prunes old backups.
// Returns the backup path, or ("", nil) when the source file does not exist.
func backupProfiles(profilesPath, backupDir string, keep int) (string, error) {
	data, err := os.ReadFile(profilesPath)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("applier: read profiles for backup: %w", err)
	}

	if err := os.MkdirAll(backupDir, 0o700); err != nil {
		return "", fmt.Errorf("applier: create backup dir: %w", err)
	}

	seq := backupSeq.Add(1)
	name := fmt.Sprintf("%s%s.%06d%s", backupPrefix, time.Now().UTC().Format(backupTimeFormat), seq, backupSuffix)
	path := filepath.Join(backupDir, name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", fmt.Errorf("applier: write backup: %w", err)
	}
	if err := pruneOldBackups(backupDir, keep); err != nil {
		return path, fmt.Errorf("applier: prune old backups: %w", err)
	}
	return path, nil
}

// latestBackup returns the newest profiles.json.*.bak in backupDir.
func latestBackup(backupDir string) (string, error) {
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		return "", fmt.Errorf("read backup dir: %w", err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		n := e.Name()
		if strings.HasPrefix(n, backupPrefix) && strings.HasSuffix(n, backupSuffix) {
			names = append(names, n)
		}
	}
	if len(names) == 0 {
		return "", fmt.Errorf("no profiles backups found in %s", backupDir)
	}
	sort.Strings(names)
	return filepath.Join(backupDir, names[len(names)-1]), nil
}

// pruneOldBackups keeps only the newest keep backup files.
func pruneOldBackups(dir string, keep int) error {
	if keep <= 0 {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read backup dir: %w", err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		n := e.Name()
		if strings.HasPrefix(n, backupPrefix) && strings.HasSuffix(n, backupSuffix) {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	if len(names) <= keep {
		return nil
	}
	for _, n := range names[:len(names)-keep] {
		if err := os.Remove(filepath.Join(dir, n)); err != nil {
			return fmt.Errorf("remove %s: %w", n, err)
		}
	}
	return nil
}
