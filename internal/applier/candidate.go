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

const candidateSubdir = "candidates"

const candidateTimeFormat = "2006-01-02T15-04-05.000000000Z"

const (
	candidatePrefix = "profiles.json.candidate."
	candidateSuffix = ".json"
)

var candidateSeq atomic.Uint64

// writeCandidate marshals pf and writes it atomically under backupDir/candidates/,
// then prunes older candidates beyond keep.
func writeCandidate(backupDir string, pf *ProfilesFile, keep int) (string, error) {
	if err := pf.Validate(); err != nil {
		return "", err
	}

	dir := filepath.Join(backupDir, candidateSubdir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("applier: create candidate dir: %w", err)
	}

	data, err := json.MarshalIndent(pf, "", "  ")
	if err != nil {
		return "", fmt.Errorf("applier: marshal candidate: %w", err)
	}
	data = append(data, '\n')

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
