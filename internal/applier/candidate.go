package applier

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"
)

// candidateSubdir is where applier stages the profiles.json it wants
// deploy/apply-profiles.sh to install, under Config.BackupDir — a
// directory the unprivileged panel process already owns and writes to,
// unlike /etc/tproxy-server itself.
const candidateSubdir = "candidates"

// candidateTimeFormat is filesystem-safe (no colons) UTC RFC3339-ish,
// matching plan.md §10's example backup filenames
// (profiles.json.2026-08-30T12-00-00Z.bak), with nanoseconds appended so
// two applies within the same second (a burst of issues/revokes) still get
// distinct candidate filenames instead of silently overwriting each other.
const candidateTimeFormat = "2006-01-02T15-04-05.000000000Z"

const candidatePrefix = "profiles.json.candidate."
const candidateSuffix = ".json"

// candidateSeq guarantees unique candidate filenames even if two writes
// land in the same nanosecond-timestamp bucket (observed under -race in
// tight test loops); it plays no role in production correctness beyond
// that.
var candidateSeq atomic.Uint64

// writeCandidate marshals pf as indented JSON and writes it atomically
// (temp file + rename, same directory as the final candidate path so the
// rename is same-filesystem) into backupDir/candidates/, then prunes
// older candidates beyond keep. It returns the path written.
//
// The rename is atomic so deploy/apply-profiles.sh — which may be invoked
// concurrently with the next write starting — never observes a
// partially-written candidate file.
func writeCandidate(backupDir string, pf *ProfilesFile, keep int) (string, error) {
	dir := filepath.Join(backupDir, candidateSubdir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("applier: create candidate dir: %w", err)
	}

	data, err := json.MarshalIndent(pf, "", "  ")
	if err != nil {
		return "", fmt.Errorf("applier: marshal candidate: %w", err)
	}

	seq := candidateSeq.Add(1)
	name := fmt.Sprintf("%s%s.%06d%s", candidatePrefix, time.Now().UTC().Format(candidateTimeFormat), seq, candidateSuffix)
	final := filepath.Join(dir, name)

	tmp, err := os.CreateTemp(dir, ".candidate-*.tmp")
	if err != nil {
		return "", fmt.Errorf("applier: create temp candidate file: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return "", fmt.Errorf("applier: write temp candidate file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("applier: close temp candidate file: %w", err)
	}
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("applier: chmod temp candidate file: %w", err)
	}
	if err := os.Rename(tmpPath, final); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("applier: rename temp candidate file: %w", err)
	}

	if err := pruneOldCandidates(dir, keep); err != nil {
		return final, fmt.Errorf("applier: prune old candidates: %w", err)
	}
	return final, nil
}

// pruneOldCandidates keeps only the newest keep candidate files in dir,
// deleting the rest. Filenames are timestamp-prefixed (candidateTimeFormat
// sorts lexically = chronologically), so a plain name sort orders oldest
// first without needing to stat mtimes.
func pruneOldCandidates(dir string, keep int) error {
	if keep <= 0 {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read candidate dir: %w", err)
	}

	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		n := e.Name()
		if strings.HasPrefix(n, candidatePrefix) && strings.HasSuffix(n, candidateSuffix) {
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
