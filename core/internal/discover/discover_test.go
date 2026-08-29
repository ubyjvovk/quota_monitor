package discover

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"quotamon/internal/config"
)

func TestCodexDiscoveryReportsFoundAndNotFoundFromATemporaryHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	deps := testDependencies(t, home)

	if finding := all(deps)[1]; finding.Found || finding.Detail != "~/.codex/auth.json not found" {
		t.Fatalf("Codex finding without auth = %#v", finding)
	}
	writeCredential(t, filepath.Join(home, ".codex", "auth.json"), `{}`)
	if finding := all(deps)[1]; !finding.Found || finding.Detail != "~/.codex/auth.json" {
		t.Fatalf("Codex finding with auth = %#v", finding)
	}
}

func TestClaudeDiscoveryUsesOnlyAStubbedSecurityExitStatus(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	deps := testDependencies(t, home)
	deps.goos = "darwin"
	var command string
	var arguments []string
	deps.run = func(name string, args ...string) error {
		command = name
		arguments = append([]string(nil), args...)
		return nil
	}

	finding := all(deps)[0]
	if !finding.Found || finding.Detail != "Keychain item present" {
		t.Fatalf("Claude finding = %#v", finding)
	}
	if command != "security" || !reflect.DeepEqual(arguments, []string{"find-generic-password", "-s", "Claude Code-credentials"}) {
		t.Fatalf("security invocation = %q %#v", command, arguments)
	}
}

func TestGrokDiscoveryReportsTheTokenExpiryFromAnExplicitAuthKey(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	now := time.Date(2026, 8, 29, 20, 0, 0, 0, time.UTC)
	deps := testDependencies(t, home)
	deps.now = func() time.Time { return now }
	writeCredential(t, filepath.Join(home, ".grok", "auth.json"), `{
  "unrelated": {"expires_at":"2030-01-01T00:00:00Z"},
  "https://auth.x.ai::client": {"key":"secret","expires_at":"2026-08-30T00:00:00Z"}
}`)

	finding := all(deps)[2]
	if !finding.Found || finding.Detail != "~/.grok/auth.json (token expires in 4h)" {
		t.Fatalf("Grok finding = %#v", finding)
	}
}

func TestDeepInfraDiscoveryFindsTheEnvironmentWithoutLoadingSecrets(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	deps := testDependencies(t, home)
	deps.getenv = func(name string) string {
		if name == "DEEPINFRA_KEY" {
			return "secret"
		}
		return ""
	}
	deps.loadConfig = func() (config.Config, error) {
		t.Fatal("config should not be loaded when the environment key is set")
		return config.Config{}, nil
	}

	finding := all(deps)[3]
	if !finding.Found || !finding.NeedsKey || finding.Detail != "DEEPINFRA_KEY set" {
		t.Fatalf("DeepInfra finding = %#v", finding)
	}
}

func TestKimiCredentialIsFoundAndReportedSupported(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeCredential(t, filepath.Join(home, ".kimi-code", "credentials", "kimi-code.json"), `{}`)

	finding := all(testDependencies(t, home))[4]
	if !finding.Found || !finding.Supported || !strings.Contains(finding.Hint, "run `kimi`") {
		t.Fatalf("Kimi finding = %#v", finding)
	}
}

func testDependencies(t *testing.T, home string) dependencies {
	t.Helper()
	return dependencies{
		goos:   "linux",
		home:   home,
		getenv: func(string) string { return "" },
		run: func(string, ...string) error {
			t.Fatal("security must be stubbed and must not run on non-Darwin")
			return errors.New("unexpected security invocation")
		},
		loadConfig: func() (config.Config, error) { return config.Config{}, config.ErrMissing },
		now:        time.Now,
	}
}

func writeCredential(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}
