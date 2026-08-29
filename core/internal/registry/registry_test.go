package registry_test

import (
	"path/filepath"
	"testing"

	"quotamon/internal/providers/claude"
	"quotamon/internal/providers/codex"
	"quotamon/internal/registry"
)

func TestAllReturnsStableProvidersAndHonoursLivePolicy(t *testing.T) {
	home := t.TempDir()
	mirrorDirectory := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("QUOTA_MONITOR_DIR", mirrorDirectory)

	providers := registry.All(registry.Options{
		LiveEnabled: func(id string) bool { return id != claude.ProviderID },
		Env:         func(string) string { return "" },
	})

	if len(providers) != 2 || providers[0].ID != claude.ProviderID || providers[1].ID != codex.ProviderID {
		t.Fatalf("All() order = %#v", providers)
	}
	if providers[0].LiveEnabled || !providers[1].LiveEnabled {
		t.Fatalf("All() live policy = Claude %v, Codex %v", providers[0].LiveEnabled, providers[1].LiveEnabled)
	}
	claudeLocal, ok := providers[0].Local.(claude.LocalSource)
	if !ok || claudeLocal.MirrorPath != filepath.Join(mirrorDirectory, "claude-usage.json") {
		t.Fatalf("Claude local = %#v", providers[0].Local)
	}
	codexLocal, ok := providers[1].Local.(codex.LocalSource)
	if !ok || codexLocal.Home != filepath.Join(home, ".codex") || codexLocal.MaxFiles != 16 || codexLocal.TailBytes != 512*1024 {
		t.Fatalf("Codex local = %#v", providers[1].Local)
	}
	if _, ok := providers[1].Live.(codex.AppServerSource); !ok {
		t.Fatalf("Codex live = %T, want AppServerSource", providers[1].Live)
	}
}

func TestAllSelectsHTTPForAnExplicitCodexUsageURL(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("QUOTA_MONITOR_DIR", t.TempDir())
	const endpoint = "https://example.test/usage"

	providers := registry.All(registry.Options{Env: func(name string) string {
		if name == "QUOTA_MONITOR_CODEX_USAGE_URL" {
			return endpoint
		}
		return ""
	}})

	live, ok := providers[1].Live.(codex.HTTPSource)
	if !ok || live.Endpoint != endpoint || live.Home != filepath.Join(home, ".codex") {
		t.Fatalf("Codex live = %#v", providers[1].Live)
	}
}
