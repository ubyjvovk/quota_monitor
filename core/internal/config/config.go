// Package config owns QuotaMon's shared, user-controlled provider configuration.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// ErrMissing reports that the mandatory configuration file does not exist.
var ErrMissing = errors.New("no config file")

// Provider contains one provider's user-selected settings.
type Provider struct {
	Enabled bool   `json:"enabled"`
	Live    string `json:"live,omitempty"`
	APIKey  string `json:"api_key,omitempty"`
}

// Config is the versioned configuration shared by every QuotaMon frontend.
type Config struct {
	Version   int                 `json:"version"`
	Providers map[string]Provider `json:"providers"`
}

// Path returns the mandatory configuration file path. QUOTA_MONITOR_DIR wins,
// followed by XDG_CONFIG_HOME and finally ~/.config on every operating system.
func Path() string {
	if directory := os.Getenv("QUOTA_MONITOR_DIR"); directory != "" {
		return filepath.Join(directory, "config.json")
	}
	if directory := os.Getenv("XDG_CONFIG_HOME"); directory != "" {
		return filepath.Join(directory, "quotamon", "config.json")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "quotamon", "config.json")
}

// Default returns version 1 configuration with every known provider disabled.
// Frontends use the complete map to offer first-run setup without maintaining
// a second provider list, while setup still decides which entries to enable.
func Default() Config {
	return Config{Version: 1, Providers: map[string]Provider{
		"claude":    {},
		"codex":     {},
		"deepinfra": {},
		"grok":      {},
		"kimi":      {},
		"runinfra":  {},
	}}
}

// Load reads the mandatory configuration. On non-Windows systems, a config
// containing any API key is rejected unless group and other access is absent.
func Load() (Config, error) {
	path := Path()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Config{}, ErrMissing
		}
		return Config{}, fmt.Errorf("read config %s: %w", path, err)
	}

	var result Config
	if err := json.Unmarshal(data, &result); err != nil {
		return Config{}, fmt.Errorf("decode config %s: %w", path, err)
	}
	if runtime.GOOS != "windows" && containsAPIKey(result) {
		info, err := os.Stat(path)
		if err != nil {
			return Config{}, fmt.Errorf("inspect config %s: %w", path, err)
		}
		if info.Mode().Perm()&0o077 != 0 {
			return Config{}, fmt.Errorf("config %s contains an API key and is accessible by other users; run `chmod 600 %s`", path, path)
		}
	}
	return result, nil
}

// Save writes the configuration atomically with stable JSON and private file
// permissions, creating its parent directory when needed.
func (c Config) Save() error {
	path := Path()
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create config directory %s: %w", directory, err)
	}

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config %s: %w", path, err)
	}
	data = append(data, '\n')

	temporary, err := os.CreateTemp(directory, ".config-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary config in %s: %w", directory, err)
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
		return fmt.Errorf("protect temporary config %s: %w", temporaryPath, err)
	}
	if _, err := temporary.Write(data); err != nil {
		return fmt.Errorf("write temporary config %s: %w", temporaryPath, err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("sync temporary config %s: %w", temporaryPath, err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary config %s: %w", temporaryPath, err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace config %s: %w", path, err)
	}
	removeTemporary = false
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("protect config %s: %w", path, err)
	}
	return nil
}

func containsAPIKey(config Config) bool {
	for _, provider := range config.Providers {
		if provider.APIKey != "" {
			return true
		}
	}
	return false
}
