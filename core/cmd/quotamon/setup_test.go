package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"quotamon/internal/config"
	"quotamon/internal/discover"
)

// noReadReader fails the test if the wizard ever reads from it, which is how
// a --yes run proves it writes without touching stdin.
type noReadReader struct {
	t *testing.T
}

func (noReadReader) Read([]byte) (int, error) {
	return 0, io.EOF
}

// stubFindings returns a fixed finding set so the wizard never touches the real
// Keychain, $HOME, or environment. It mirrors what discovery looks like on this
// machine: Claude and ChatGPT signed in, Grok and DeepInfra not, Kimi found but
// unsupported.
func stubFindings() []discover.Finding {
	return []discover.Finding{
		{ID: "claude", DisplayName: "Claude", Found: true, Supported: true, Detail: "Keychain item present"},
		{ID: "codex", DisplayName: "ChatGPT", Found: true, Supported: true, Detail: "~/.codex/auth.json"},
		{ID: "grok", DisplayName: "Grok", Found: false, Supported: true, Detail: "~/.grok/auth.json not found", Hint: "run `grok login`"},
		{ID: "deepinfra", DisplayName: "DeepInfra", Found: false, Supported: true, NeedsKey: true, Detail: "DEEPINFRA_KEY not set"},
		{ID: "kimi", DisplayName: "Kimi", Found: true, Supported: false, Detail: "~/.kimi-code/credentials/kimi-code.json"},
	}
}

func TestSetupWritesTheExpectedConfigFromScriptedStdinAndProtectsIt(t *testing.T) {
	t.Setenv("QUOTA_MONITOR_DIR", t.TempDir())
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	// claude: blank keeps the found default (on); codex: n disables it;
	// grok: blank keeps the not-found default (off); deepinfra: y enables it
	// and abc123 answers its key prompt; final n declines a manual add.
	exit := runSetup(strings.NewReader("\nn\n\ny\nabc123\nn\n"), &stdout, &stderr, false, stubFindings)
	if exit != 0 {
		t.Fatalf("setup exit = %d, stderr = %q", exit, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Looking for providers") ||
		!strings.Contains(stdout.String(), "Wrote ") || !strings.Contains(stdout.String(), "(mode 0600). Run: quotamon") {
		t.Fatalf("setup stdout = %q", stdout.String())
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Providers["claude"].Enabled {
		t.Errorf("claude enabled = %v, want true", cfg.Providers["claude"].Enabled)
	}
	if cfg.Providers["grok"].Enabled {
		t.Errorf("grok enabled = %v, want false", cfg.Providers["grok"].Enabled)
	}
	if !cfg.Providers["deepinfra"].Enabled || cfg.Providers["deepinfra"].APIKey != "abc123" {
		t.Errorf("deepinfra = %#v, want enabled with api_key abc123", cfg.Providers["deepinfra"])
	}
	if _, ok := cfg.Providers["kimi"]; ok {
		t.Errorf("unsupported kimi got a config entry: %#v", cfg.Providers["kimi"])
	}

	if runtime.GOOS != "windows" {
		info, err := os.Stat(config.Path())
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Errorf("config mode = %04o, want 0600", info.Mode().Perm())
		}
	}
}

func TestSetupYesRewritesWithoutReadingStdinAndKeepsThePastedKey(t *testing.T) {
	t.Setenv("QUOTA_MONITOR_DIR", t.TempDir())

	// First an interactive run that stores the DeepInfra key on disk.
	var first bytes.Buffer
	if exit := runSetup(strings.NewReader("\n\n\ny\nabc123\nn\n"), &first, io.Discard, false, stubFindings); exit != 0 {
		t.Fatalf("interactive setup exit = %d", exit)
	}

	// Re-running with --yes must not touch stdin at all (noReadReader fails the
	// test on any read) and must preserve the key it already has.
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exit := runSetup(noReadReader{t: t}, &stdout, &stderr, true, stubFindings)
	if exit != 0 {
		t.Fatalf("setup --yes exit = %d, stderr = %q", exit, stderr.String())
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Providers["claude"].Enabled {
		t.Errorf("claude enabled = %v, want true (found)", cfg.Providers["claude"].Enabled)
	}
	if cfg.Providers["grok"].Enabled {
		t.Errorf("grok enabled = %v, want false (not found)", cfg.Providers["grok"].Enabled)
	}
	if !cfg.Providers["deepinfra"].Enabled || cfg.Providers["deepinfra"].APIKey != "abc123" {
		t.Errorf("deepinfra = %#v, want still enabled with api_key abc123", cfg.Providers["deepinfra"])
	}
}

func TestSetupYesOnAFreshRunEnablesOnlyWhatWasFoundWithoutAKeyPrompt(t *testing.T) {
	t.Setenv("QUOTA_MONITOR_DIR", t.TempDir())
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exit := runSetup(noReadReader{t: t}, &stdout, &stderr, true, stubFindings)
	if exit != 0 {
		t.Fatalf("setup --yes exit = %d, stderr = %q", exit, stderr.String())
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Providers["claude"].Enabled {
		t.Errorf("claude enabled = %v, want true", cfg.Providers["claude"].Enabled)
	}
	if cfg.Providers["grok"].Enabled {
		t.Errorf("grok enabled = %v, want false", cfg.Providers["grok"].Enabled)
	}
	// DeepInfra was not found, so --yes never prompts for a key and leaves it off.
	if cfg.Providers["deepinfra"].Enabled || cfg.Providers["deepinfra"].APIKey != "" {
		t.Errorf("deepinfra = %#v, want disabled with no key", cfg.Providers["deepinfra"])
	}
}

// kimiSupportedFindings mirrors stubFindings but treats Kimi as supported and
// found, the state after Kimi shipped with credentials present.
func kimiSupportedFindings() []discover.Finding {
	findings := stubFindings()
	for i := range findings {
		if findings[i].ID == "kimi" {
			findings[i].Supported = true
		}
	}
	return findings
}

// A config written before Kimi was supported pins it off; now that it is found
// and supported, setup --yes must adopt it (override the stale false) and say so
// in its output rather than silently leaving it off.
func TestSetupYesAdoptsANewlySupportedFoundProviderWhoseEntryWasOff(t *testing.T) {
	directory := t.TempDir()
	t.Setenv("QUOTA_MONITOR_DIR", directory)
	stale := config.Config{Version: 1, Providers: map[string]config.Provider{"kimi": {Enabled: false}}}
	if err := stale.Save(); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exit := runSetup(noReadReader{t: t}, &stdout, &stderr, true, kimiSupportedFindings)
	if exit != 0 {
		t.Fatalf("setup --yes exit = %d, stderr = %q", exit, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Enabled every provider that was found; run without --yes to choose.") {
		t.Fatalf("setup --yes stdout = %q, want the run-without---yes line", stdout.String())
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Providers["kimi"].Enabled {
		t.Errorf("kimi enabled = %v, want true after --yes", cfg.Providers["kimi"].Enabled)
	}
}

func TestSetupPrintsAnUnsupportedFindingSoTheUserIsNotLeftWondering(t *testing.T) {
	t.Setenv("QUOTA_MONITOR_DIR", t.TempDir())
	var stdout bytes.Buffer
	if exit := runSetup(strings.NewReader("\n\n\n\n\nn\n"), &stdout, io.Discard, false, stubFindings); exit != 0 {
		t.Fatalf("setup exit = %d", exit)
	}
	if !strings.Contains(stdout.String(), "Kimi") ||
		!strings.Contains(stdout.String(), "credentials found, but Kimi exposes no quota API yet") {
		t.Fatalf("setup stdout = %q, want the Kimi not-supported line", stdout.String())
	}
}

// A malformed on-disk config means a load error rather than first-run, and
// overwriting it with defaults would destroy whatever the user had stored. Setup
// must therefore refuse to write and leave the file byte-identical.
func TestSetupRefusesToOverwriteAMalformedConfigAndLeavesItByteIdentical(t *testing.T) {
	directory := t.TempDir()
	t.Setenv("QUOTA_MONITOR_DIR", directory)
	path := filepath.Join(directory, "config.json")
	garbage := []byte("{ this is not json\n")
	if err := os.WriteFile(path, garbage, 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exit := runSetup(strings.NewReader("\n\n\n\n\nn\n"), &stdout, &stderr, false, stubFindings)
	if exit == 0 {
		t.Fatalf("setup exit = 0, want non-zero; stderr = %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "setup: refusing to overwrite") {
		t.Errorf("stderr = %q, want the refusing-to-overwrite line", stderr.String())
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, garbage) {
		t.Errorf("config was rewritten; got %q, want %q", after, garbage)
	}
}

// A config carrying an API key whose loose permissions config.Load rejects must
// not be silently overwritten either; the key is the whole point of the file.
func TestSetupRefusesToOverwriteALoosePermsConfigWithAStoredKey(t *testing.T) {
	directory := t.TempDir()
	t.Setenv("QUOTA_MONITOR_DIR", directory)
	path := filepath.Join(directory, "config.json")
	contents := []byte("{\n  \"version\": 1,\n  \"providers\": {\n    \"deepinfra\": {\n      \"enabled\": true,\n      \"api_key\": \"secret\"\n    }\n  }\n}\n")
	if err := os.WriteFile(path, contents, 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exit := runSetup(strings.NewReader("\n\n\n\n\nn\n"), &stdout, &stderr, false, stubFindings)
	if exit == 0 {
		t.Fatalf("setup exit = 0, want non-zero; stderr = %q", stderr.String())
	}
	if runtime.GOOS == "windows" {
		return // perms enforcement is skipped on Windows, so there is nothing to refuse
	}
	if !strings.Contains(stderr.String(), "chmod 600") {
		t.Errorf("stderr = %q, want the chmod 600 fix hint", stderr.String())
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, contents) {
		t.Errorf("config was rewritten; got %q, want %q", after, contents)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Errorf("config mode = %04o, want 0644 (untouched)", info.Mode().Perm())
	}
}

// A missing config is the genuine first-run case; setup must proceed as before
// and end with a loadable config on disk.
func TestSetupProceedsWhenThereIsNoConfigYet(t *testing.T) {
	t.Setenv("QUOTA_MONITOR_DIR", t.TempDir())
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exit := runSetup(strings.NewReader("\n\n\n\n\nn\n"), &stdout, &stderr, false, stubFindings)
	if exit != 0 {
		t.Fatalf("setup exit = %d, want 0; stderr = %q", exit, stderr.String())
	}
	if _, err := config.Load(); err != nil {
		t.Fatalf("expected setup to write a loadable config: %v", err)
	}
}

func TestProvidersPrintsTheTableWithAnEnabledColumn(t *testing.T) {
	directory := t.TempDir()
	t.Setenv("QUOTA_MONITOR_DIR", directory)
	cfg := config.Default()
	cfg.Providers["claude"] = config.Provider{Enabled: true}
	cfg.Providers["codex"] = config.Provider{Enabled: true}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if exit := runProviders(&stdout, &stderr, stubFindings); exit != 0 {
		t.Fatalf("providers exit = %d, stderr = %q", exit, stderr.String())
	}
	for _, want := range []string{"Claude", "  on  ", "ChatGPT", "Grok", "  off ", "DeepInfra"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("providers output missing %q: %q", want, stdout.String())
		}
	}
}

func TestProvidersWithoutAConfigExitsThreeWithTheSetupHint(t *testing.T) {
	t.Setenv("QUOTA_MONITOR_DIR", t.TempDir())
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if exit := runProviders(&stdout, &stderr, stubFindings); exit != 3 {
		t.Fatalf("providers without config exit = %d, want 3", exit)
	}
	if !strings.Contains(stderr.String(), "run: quotamon setup") {
		t.Fatalf("providers stderr = %q, want setup hint", stderr.String())
	}
}
