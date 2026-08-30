package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"quotamon/internal/config"
	"quotamon/internal/discover"
)

func TestDiscoverJSONPrintsEveryDocumentedFieldInRegistryOrder(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("QUOTA_MONITOR_DIR", t.TempDir())
	findings := []discover.Finding{
		{ID: "claude", DisplayName: "Claude", Found: true, Supported: true, Detail: "Keychain item present", Hint: "run `claude`", NeedsKey: false},
		{ID: "codex", DisplayName: "ChatGPT", Found: false, Supported: true, Detail: "~/.codex/auth.json not found", Hint: "run `codex login`", NeedsKey: false},
		{ID: "grok", DisplayName: "Grok", Found: false, Supported: true, Detail: "~/.grok/auth.json not found", Hint: "run `grok login`", NeedsKey: false},
		{ID: "deepinfra", DisplayName: "DeepInfra", Found: false, Supported: true, Detail: "DEEPINFRA_KEY not set", Hint: "get a key", NeedsKey: true},
		{ID: "kimi", DisplayName: "Kimi", Found: false, Supported: true, Detail: "credentials not found", Hint: "run `kimi`", NeedsKey: false},
	}
	probeCalls := 0
	allFindings := func() []discover.Finding {
		probeCalls++
		return findings
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exit := runWithDependencies([]string{"discover", "--json"}, strings.NewReader(""), &stdout, &stderr, time.Now, fixedFactory(nil), allFindings)
	if exit != 0 || stderr.Len() != 0 {
		t.Fatalf("discover --json exit = %d, stderr = %q", exit, stderr.String())
	}
	if probeCalls != 1 {
		t.Fatalf("injected local probe calls = %d, want 1", probeCalls)
	}

	var decoded []map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &decoded); err != nil {
		t.Fatalf("discover --json output is not valid JSON: %v", err)
	}
	if len(decoded) != len(findings) {
		t.Fatalf("discover --json returned %d findings, want %d", len(decoded), len(findings))
	}
	wantKeys := []string{"id", "displayName", "found", "supported", "detail", "hint", "needsKey"}
	for index, finding := range decoded {
		if got := finding["id"]; got != findings[index].ID {
			t.Errorf("finding %d id = %#v, want %q", index, got, findings[index].ID)
		}
		if len(finding) != len(wantKeys) {
			t.Errorf("finding %d has keys %#v", index, finding)
		}
		for _, key := range wantKeys {
			if _, found := finding[key]; !found {
				t.Errorf("finding %d is missing key %q: %#v", index, key, finding)
			}
		}
	}
}

func TestConfigPathHonoursQuotaMonitorDirectory(t *testing.T) {
	directory := t.TempDir()
	t.Setenv("QUOTA_MONITOR_DIR", directory)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if exit := run([]string{"config", "path"}, strings.NewReader(""), &stdout, &stderr, time.Now); exit != 0 {
		t.Fatalf("config path exit = %d, stderr = %q", exit, stderr.String())
	}
	if got, want := stdout.String(), filepath.Join(directory, "config.json")+"\n"; got != want {
		t.Fatalf("config path stdout = %q, want %q", got, want)
	}
}

func TestConfigGetJSONPrintsSortedEffectiveDefaultsAndMergesAFile(t *testing.T) {
	directory := t.TempDir()
	t.Setenv("QUOTA_MONITOR_DIR", directory)

	stdout := runConfigGetJSON(t)
	assertSortedProviderKeys(t, stdout)
	var defaults config.Config
	if err := json.Unmarshal([]byte(stdout), &defaults); err != nil {
		t.Fatalf("config get --json defaults are not JSON: %v", err)
	}
	if len(defaults.Providers) != 5 {
		t.Fatalf("default provider count = %d, want 5", len(defaults.Providers))
	}
	for id, provider := range defaults.Providers {
		if provider.Enabled {
			t.Errorf("default provider %q is enabled", id)
		}
	}

	data := []byte(`{"version":1,"providers":{"claude":{"enabled":true},"codex":{"enabled":false,"live":"http"},"future":{"enabled":true}}}`)
	if err := os.WriteFile(filepath.Join(directory, "config.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	stdout = runConfigGetJSON(t)
	assertSortedProviderKeys(t, stdout)
	var merged config.Config
	if err := json.Unmarshal([]byte(stdout), &merged); err != nil {
		t.Fatalf("config get --json merged output is not JSON: %v", err)
	}
	if !merged.Providers["claude"].Enabled || merged.Providers["codex"].Live != "http" || !merged.Providers["future"].Enabled {
		t.Fatalf("merged config = %#v", merged)
	}
	if _, found := merged.Providers["deepinfra"]; !found {
		t.Fatalf("merged config omitted default provider: %#v", merged.Providers)
	}
}

func TestConfigSetWritesPrivateFileWithoutEchoingKeyAndPreservesEarlierEntries(t *testing.T) {
	directory := t.TempDir()
	t.Setenv("QUOTA_MONITOR_DIR", directory)

	stdout := runSuccessfulConfigCommand(t, []string{"config", "set", "deepinfra", "--enabled=true", "--api-key=SECRET"})
	if strings.Contains(stdout, "SECRET") {
		t.Fatalf("config set echoed API key: %q", stdout)
	}
	info, err := os.Stat(filepath.Join(directory, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("config mode = %04o, want 0600", info.Mode().Perm())
	}

	stdout = runSuccessfulConfigCommand(t, []string{"config", "set", "claude", "--enabled=true"})
	if strings.Contains(stdout, "SECRET") {
		t.Fatalf("second config set echoed stored API key: %q", stdout)
	}
	loaded, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.Providers["deepinfra"].Enabled || loaded.Providers["deepinfra"].APIKey != "SECRET" {
		t.Fatalf("deepinfra entry was not preserved: %#v", loaded.Providers["deepinfra"])
	}
	if !loaded.Providers["claude"].Enabled {
		t.Fatalf("claude entry = %#v, want enabled", loaded.Providers["claude"])
	}
}

func TestConfigSetRejectsInvalidModesUnknownProvidersAndMissingFlags(t *testing.T) {
	t.Setenv("QUOTA_MONITOR_DIR", t.TempDir())
	tests := []struct {
		name string
		args []string
	}{
		{name: "invalid codex live mode", args: []string{"config", "set", "codex", "--live=bogus"}},
		{name: "unknown provider", args: []string{"config", "set", "nosuch", "--enabled=true"}},
		{name: "no setting flags", args: []string{"config", "set", "claude"}},
		{name: "live mode on non-codex provider", args: []string{"config", "set", "claude", "--live=off"}},
		{name: "unknown config subcommand", args: []string{"config", "remove"}},
		{name: "bad get flag", args: []string{"config", "get", "--yaml"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			exit := run(test.args, strings.NewReader(""), &stdout, &stderr, time.Now)
			if exit != 2 || stderr.Len() == 0 {
				t.Fatalf("run(%q) exit = %d, stdout = %q, stderr = %q", test.args, exit, stdout.String(), stderr.String())
			}
		})
	}
}

func runConfigGetJSON(t *testing.T) string {
	t.Helper()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if exit := run([]string{"config", "get", "--json"}, strings.NewReader(""), &stdout, &stderr, time.Now); exit != 0 {
		t.Fatalf("config get --json exit = %d, stderr = %q", exit, stderr.String())
	}
	return stdout.String()
}

func assertSortedProviderKeys(t *testing.T, output string) {
	t.Helper()
	last := -1
	for _, id := range []string{"claude", "codex", "deepinfra", "grok", "kimi"} {
		index := strings.Index(output, `"`+id+`"`)
		if index < last || index < 0 {
			t.Fatalf("provider keys are not sorted in %q", output)
		}
		last = index
	}
}

func runSuccessfulConfigCommand(t *testing.T, args []string) string {
	t.Helper()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if exit := run(args, strings.NewReader(""), &stdout, &stderr, time.Now); exit != 0 {
		t.Fatalf("run(%q) exit = %d, stderr = %q", args, exit, stderr.String())
	}
	if !strings.HasPrefix(stdout.String(), "wrote ") {
		t.Fatalf("run(%q) stdout = %q", args, stdout.String())
	}
	return stdout.String()
}
