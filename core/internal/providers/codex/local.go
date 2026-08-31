package codex

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"quotamon/internal/format"
	"quotamon/internal/jsonx"
	"quotamon/internal/snapshot"
	"quotamon/internal/source"
)

const (
	defaultMaxFiles  = 16
	defaultTailBytes = 512 * 1024
	// defaultMaxCandidates bounds how many "rate_limits" lines one rollout may
	// offer per pass, so a transcript that discusses rate limits at length cannot
	// turn a scan into a full-file JSON parse marathon.
	defaultMaxCandidates = 32
)

// LocalSource reads the newest cached rate limits from Codex rollout files.
type LocalSource struct {
	// Home is the Codex CLI data directory.
	Home string
	// MaxFiles limits how many newest rollout files are inspected.
	MaxFiles int
	// TailBytes limits the initial read from each rollout's tail.
	TailBytes int
	// Now supplies the current time for rollover status checks.
	Now func() time.Time
}

// ProviderID returns the stable ChatGPT provider identifier.
func (LocalSource) ProviderID() string { return ProviderID }

// DisplayName returns the provider name shown to users.
func (LocalSource) DisplayName() string { return DisplayName }

// Origin identifies rollout readings as local.
func (LocalSource) Origin() snapshot.Origin { return snapshot.OriginLocal }

// Fetch returns the last usable rate-limit record from the newest rollouts.
func (s LocalSource) Fetch(context.Context) (snapshot.Provider, error) {
	home := s.Home
	if home == "" {
		home = DefaultHome()
	}
	maxFiles := s.MaxFiles
	if maxFiles <= 0 {
		maxFiles = defaultMaxFiles
	}
	tailBytes := s.TailBytes
	if tailBytes <= 0 {
		tailBytes = defaultTailBytes
	}
	now := s.Now
	if now == nil {
		now = time.Now
	}

	sessionsRoot := filepath.Join(home, "sessions")
	if info, err := os.Stat(sessionsRoot); err != nil || !info.IsDir() {
		if err == nil || os.IsNotExist(err) {
			return snapshot.Provider{}, source.Errorf(source.NotConfigured, "No Codex sessions found at %s", sessionsRoot)
		}
		return snapshot.Provider{}, source.Errorf(source.Transport, "Read Codex sessions at %s: %v", sessionsRoot, err)
	}

	rollouts, err := newestRollouts(sessionsRoot, maxFiles)
	if err != nil {
		return snapshot.Provider{}, source.Errorf(source.Transport, "List Codex sessions at %s: %v", sessionsRoot, err)
	}
	var firstReadError error
	var firstReadPath string
	for _, rollout := range rollouts {
		var provider snapshot.Provider
		found, err := eachCandidate(rollout.path, "rate_limits", tailBytes, defaultMaxCandidates, func(line string) bool {
			candidate, ok := rolloutRecord(line, rollout.modified)
			if !ok {
				return false
			}
			provider = candidate
			return true
		})
		if err != nil {
			// One unreadable rollout — a permission-denied session directory, a file
			// rotated away mid-scan — used to fail the whole source. Skip it and keep
			// the reason, which is only reported if no rollout at all yields a reading.
			if firstReadError == nil {
				firstReadError, firstReadPath = err, rollout.path
			}
			continue
		}
		if !found {
			continue
		}

		observedAt := provider.ObservedAt.Time
		observedNow := now()
		current := false
		for _, window := range provider.Windows {
			if _, valid := window.CurrentUsedPercent(observedNow); valid {
				current = true
				break
			}
		}
		if !current {
			provider.Status = snapshot.NeedsSetup(
				"ChatGPT reports usage only after a Codex turn — last reading " + format.Age(observedNow.Sub(observedAt)),
			)
		}
		return provider, nil
	}

	if firstReadError != nil {
		return snapshot.Provider{}, source.Errorf(source.Transport, "Read Codex session %s: %v", firstReadPath, firstReadError)
	}
	return snapshot.Provider{}, source.Errorf(source.NoDataFound, "No rate limit records in the last %d Codex sessions", maxFiles)
}

// rolloutRecord normalises one rollout line that carries a payload.rate_limits record.
func rolloutRecord(line string, modified time.Time) (snapshot.Provider, bool) {
	root, err := jsonx.Parse([]byte(line))
	if err != nil {
		return snapshot.Provider{}, false
	}
	limits, ok := jsonx.Get(root, "payload", "rate_limits")
	if !ok {
		return snapshot.Provider{}, false
	}
	observedAt := modified
	if value, found := jsonx.Get(root, "timestamp"); found {
		if parsed, valid := jsonx.Time(value); valid {
			observedAt = parsed
		}
	}
	return Snapshot(limits, observedAt, snapshot.OriginLocal)
}

// LastLine returns the last whole line containing needle, reading the tail first.
func LastLine(path string, needle string, tailBytes int) (string, bool, error) {
	var line string
	found, err := eachCandidate(path, needle, tailBytes, 1, func(candidate string) bool {
		line = candidate
		return true
	})
	return line, found, err
}

// eachCandidate offers whole lines containing needle to accept, newest first, and
// stops at the first line accept keeps. The file's tail is read first because a
// rollout is mostly transcript; only when every tail candidate is rejected does the
// whole file get re-read, so a decoy tail line — chat text that merely quotes
// "rate_limits" — cannot hide the real record earlier in the same rollout. At most
// maximum lines are offered per pass.
func eachCandidate(path, needle string, tailBytes, maximum int, accept func(line string) bool) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	if tailBytes > 0 && info.Size() > int64(tailBytes) {
		offset := info.Size() - int64(tailBytes)
		if kept, err := offerFromOffset(path, needle, offset, true, maximum, accept); err != nil || kept {
			return kept, err
		}
	}
	return offerFromOffset(path, needle, 0, false, maximum, accept)
}

type rolloutFile struct {
	path     string
	modified time.Time
}

func newestRollouts(root string, maximum int) ([]rolloutFile, error) {
	var files []rolloutFile
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(path) != ".jsonl" {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			files = append(files, rolloutFile{path: path, modified: info.ModTime()})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(files, func(left, right int) bool {
		if files[left].modified.Equal(files[right].modified) {
			return files[left].path > files[right].path
		}
		return files[left].modified.After(files[right].modified)
	})
	if len(files) > maximum {
		files = files[:maximum]
	}
	return files, nil
}

// offerFromOffset walks one read of the file backwards, offering matching whole
// lines to accept. dropLeading discards the partial first line of a mid-file read.
func offerFromOffset(path, needle string, offset int64, dropLeading bool, maximum int, accept func(line string) bool) (bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer file.Close()
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return false, err
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return false, err
	}
	text := string(data)
	if dropLeading {
		firstBreak := strings.IndexByte(text, '\n')
		if firstBreak < 0 {
			return false, nil
		}
		text = text[firstBreak+1:]
	}
	lines := strings.Split(text, "\n")
	offered := 0
	for index := len(lines) - 1; index >= 0 && offered < maximum; index-- {
		if lines[index] == "" || !strings.Contains(lines[index], needle) {
			continue
		}
		offered++
		if accept(lines[index]) {
			return true, nil
		}
	}
	return false, nil
}
