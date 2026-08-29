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
	for _, rollout := range rollouts {
		line, found, err := LastLine(rollout.path, "rate_limits", tailBytes)
		if err != nil {
			return snapshot.Provider{}, source.Errorf(source.Transport, "Read Codex session %s: %v", rollout.path, err)
		}
		if !found {
			continue
		}
		root, err := jsonx.Parse([]byte(line))
		if err != nil {
			continue
		}
		limits, ok := jsonx.Get(root, "payload", "rate_limits")
		if !ok {
			continue
		}
		observedAt := rollout.modified
		if value, found := jsonx.Get(root, "timestamp"); found {
			if parsed, valid := jsonx.Time(value); valid {
				observedAt = parsed
			}
		}
		provider, ok := Snapshot(limits, observedAt, snapshot.OriginLocal)
		if !ok {
			continue
		}

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

	return snapshot.Provider{}, source.Errorf(source.NoDataFound, "No rate limit records in the last %d Codex sessions", maxFiles)
}

// LastLine returns the last whole line containing needle, reading the tail first.
func LastLine(path string, needle string, tailBytes int) (string, bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", false, err
	}
	if tailBytes <= 0 || info.Size() <= int64(tailBytes) {
		return lastLineFromOffset(path, needle, 0, false)
	}

	offset := info.Size() - int64(tailBytes)
	if line, ok, err := lastLineFromOffset(path, needle, offset, true); err != nil || ok {
		return line, ok, err
	}
	return lastLineFromOffset(path, needle, 0, false)
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

func lastLineFromOffset(path, needle string, offset int64, dropLeading bool) (string, bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", false, err
	}
	defer file.Close()
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return "", false, err
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return "", false, err
	}
	text := string(data)
	if dropLeading {
		firstBreak := strings.IndexByte(text, '\n')
		if firstBreak < 0 {
			return "", false, nil
		}
		text = text[firstBreak+1:]
	}
	lines := strings.Split(text, "\n")
	for index := len(lines) - 1; index >= 0; index-- {
		if lines[index] != "" && strings.Contains(lines[index], needle) {
			return lines[index], true, nil
		}
	}
	return "", false, nil
}
