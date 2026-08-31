package config

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestSaveThenLoadRoundTripsWithPrivateStableJSON(t *testing.T) {
	directory := t.TempDir()
	t.Setenv("QUOTA_MONITOR_DIR", directory)
	input := Default()
	input.Providers["claude"] = Provider{Enabled: true}
	input.Providers["codex"] = Provider{Enabled: true, Live: "app-server"}
	input.Providers["deepinfra"] = Provider{Enabled: true, APIKey: "secret"}

	if err := input.Save(); err != nil {
		t.Fatal(err)
	}
	output, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(output, input) {
		t.Fatalf("Load() = %#v, want %#v", output, input)
	}

	info, err := os.Stat(Path())
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("config mode = %04o, want 0600", info.Mode().Perm())
	}
	data, err := os.ReadFile(Path())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Index(string(data), `"claude"`) > strings.Index(string(data), `"codex"`) ||
		strings.Index(string(data), `"codex"`) > strings.Index(string(data), `"deepinfra"`) {
		t.Fatalf("provider keys are not sorted: %s", data)
	}
}

func TestLoadRefusesAnExposedAPIKey(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no Unix permission bits")
	}
	directory := t.TempDir()
	t.Setenv("QUOTA_MONITOR_DIR", directory)
	path := filepath.Join(directory, "config.json")
	data := []byte(`{"version":1,"providers":{"deepinfra":{"enabled":true,"api_key":"secret"}}}`)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), "chmod 600") {
		t.Fatalf("Load() error = %v, want file path and chmod 600", err)
	}
}

func TestLoadReportsErrMissingForAnAbsentFile(t *testing.T) {
	t.Setenv("QUOTA_MONITOR_DIR", t.TempDir())
	_, err := Load()
	if !errors.Is(err, ErrMissing) {
		t.Fatalf("Load() error = %v, want ErrMissing", err)
	}
}

func TestQuotaMonitorDirectoryOverridesTheConfigPath(t *testing.T) {
	directory := t.TempDir()
	t.Setenv("QUOTA_MONITOR_DIR", directory)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "xdg"))
	t.Setenv("HOME", filepath.Join(t.TempDir(), "home"))
	if got, want := Path(), filepath.Join(directory, "config.json"); got != want {
		t.Fatalf("Path() = %q, want %q", got, want)
	}
}

func TestDefaultHasEveryProviderDisabled(t *testing.T) {
	providers := Default().Providers
	for _, id := range []string{"claude", "codex", "deepinfra", "grok", "kimi", "runinfra"} {
		provider, found := providers[id]
		if !found {
			t.Errorf("Default().Providers is missing %q", id)
		} else if provider.Enabled {
			t.Errorf("Default().Providers[%q].Enabled = true, want false", id)
		}
	}
	if got := len(providers); got != 6 {
		t.Fatalf("Default().Providers has %d entries, want 6", got)
	}
}

// `config set <id>` validates its argument against Default().Providers, so a
// provider missing from that map is unconfigurable from the CLI however well
// the registry knows it — RunInfra shipped that way once and `config set
// runinfra --api-key-stdin` answered `unknown provider "runinfra"`.
func TestEveryDefaultProviderRoundTripsAnEnabledAPIKey(t *testing.T) {
	for id := range Default().Providers {
		t.Run(id, func(t *testing.T) {
			t.Setenv("QUOTA_MONITOR_DIR", t.TempDir())
			input := Default()
			input.Providers[id] = Provider{Enabled: true, APIKey: "secret-" + id}

			if err := input.Save(); err != nil {
				t.Fatal(err)
			}
			output, err := Load()
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(output.Providers[id], input.Providers[id]) {
				t.Fatalf("Load().Providers[%q] = %#v, want %#v", id, output.Providers[id], input.Providers[id])
			}
		})
	}
}

// Load must tolerate an entry for a provider the registry does not know yet so
// an older config keeps working once a provider ships (or is removed) and setup
// can still read and rewrite it.
func TestLoadIgnoresAnUnknownProviderEntry(t *testing.T) {
	directory := t.TempDir()
	t.Setenv("QUOTA_MONITOR_DIR", directory)
	path := filepath.Join(directory, "config.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"providers":{"nope":{"enabled":true},"claude":{"enabled":true}}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v, want the unknown id ignored", err)
	}
	if !result.Providers["claude"].Enabled {
		t.Errorf("claude enabled = %v, want true", result.Providers["claude"].Enabled)
	}
	if !result.Providers["nope"].Enabled {
		t.Errorf("unknown id was dropped by Load: %#v", result.Providers)
	}
}
