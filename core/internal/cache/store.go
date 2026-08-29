// Package cache persists the last usable live reading for each provider.
package cache

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"quotamon/internal/config"
	"quotamon/internal/snapshot"
)

// Store keeps one private JSON snapshot per provider. An empty Dir uses the
// cache directory beside QuotaMon's configuration file.
type Store struct {
	Dir string
}

// Load returns a provider's last saved snapshot. Missing, unreadable, or
// malformed entries are cache misses so a damaged cache never blocks fetching.
func (s Store) Load(providerID string) (snapshot.Provider, bool) {
	data, err := os.ReadFile(s.path(providerID))
	if err != nil {
		return snapshot.Provider{}, false
	}
	var provider snapshot.Provider
	if err := json.Unmarshal(data, &provider); err != nil {
		return snapshot.Provider{}, false
	}
	return provider, true
}

// Save atomically replaces a provider's cached snapshot with a private file.
func (s Store) Save(provider snapshot.Provider) error {
	directory := s.directory()
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create quota cache directory %s: %w", directory, err)
	}

	data, err := json.Marshal(provider)
	if err != nil {
		return fmt.Errorf("encode %s quota cache: %w", provider.ID, err)
	}
	data = append(data, '\n')

	temporary, err := os.CreateTemp(directory, ".quota-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary quota cache in %s: %w", directory, err)
	}
	temporaryPath := temporary.Name()
	removeTemporary := true
	defer func() {
		_ = temporary.Close()
		if removeTemporary {
			_ = os.Remove(temporaryPath)
		}
	}()

	if err := temporary.Chmod(0o600); err != nil {
		return fmt.Errorf("protect temporary quota cache %s: %w", temporaryPath, err)
	}
	if _, err := temporary.Write(data); err != nil {
		return fmt.Errorf("write temporary quota cache %s: %w", temporaryPath, err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("sync temporary quota cache %s: %w", temporaryPath, err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary quota cache %s: %w", temporaryPath, err)
	}
	path := s.path(provider.ID)
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace quota cache %s: %w", path, err)
	}
	removeTemporary = false
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("protect quota cache %s: %w", path, err)
	}
	return nil
}

func (s Store) directory() string {
	if s.Dir != "" {
		return s.Dir
	}
	return filepath.Join(filepath.Dir(config.Path()), "cache")
}

func (s Store) path(providerID string) string {
	return filepath.Join(s.directory(), providerID+".json")
}
