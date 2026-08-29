package registry_test

import (
	"path/filepath"
	"testing"

	"quotamon/internal/config"
	"quotamon/internal/providers/claude"
	"quotamon/internal/providers/codex"
	"quotamon/internal/providers/deepinfra"
	"quotamon/internal/providers/grok"
	"quotamon/internal/registry"
)

// enabledConfig returns a configuration that turns every known provider on,
// leaving each provider's provider-specific settings at their defaults.
func enabledConfig() config.Config {
	providers := map[string]config.Provider{}
	for _, id := range []string{claude.ProviderID, codex.ProviderID, grok.ProviderID, deepinfra.ProviderID} {
		providers[id] = config.Provider{Enabled: true}
	}
	return config.Config{Version: 1, Providers: providers}
}

func TestAllReturnsOnlyEnabledProvidersInStableOrder(t *testing.T) {
	home := t.TempDir()
	mirrorDirectory := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("QUOTA_MONITOR_DIR", mirrorDirectory)

	providers := registry.All(registry.Options{
		Config: enabledConfig(),
		Env:    func(string) string { return "" },
	})

	if len(providers) != 4 || providers[0].ID != claude.ProviderID || providers[1].ID != codex.ProviderID || providers[2].ID != grok.ProviderID || providers[3].ID != deepinfra.ProviderID {
		t.Fatalf("All() order = %#v", providers)
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
	if providers[2].Local != nil {
		t.Fatalf("Grok local = %T, want nil", providers[2].Local)
	}
	if _, ok := providers[2].Live.(grok.LiveSource); !ok {
		t.Fatalf("Grok live = %T, want LiveSource", providers[2].Live)
	}
	if providers[3].Local != nil {
		t.Fatalf("DeepInfra local = %T, want nil", providers[3].Local)
	}
	if _, ok := providers[3].Live.(deepinfra.LiveSource); !ok {
		t.Fatalf("DeepInfra live = %T, want LiveSource", providers[3].Live)
	}
}

func TestAllAppliesLivePolicyAcrossEnabledProviders(t *testing.T) {
	providers := registry.All(registry.Options{
		Config: enabledConfig(),
		LiveEnabled: func(id string) bool { return id != claude.ProviderID },
	})
	if providers[0].LiveEnabled || !providers[1].LiveEnabled || !providers[2].LiveEnabled || !providers[3].LiveEnabled {
		t.Fatalf("All() live policy = %v %v %v %v, want only Claude disabled", providers[0].LiveEnabled, providers[1].LiveEnabled, providers[2].LiveEnabled, providers[3].LiveEnabled)
	}
}

func TestAllOmitsDisabledProviders(t *testing.T) {
	cfg := enabledConfig()
	cfg.Providers["claude"] = config.Provider{Enabled: false}
	cfg.Providers["deepinfra"] = config.Provider{Enabled: false}

	providers := registry.All(registry.Options{Config: cfg})
	ids := make([]string, 0, len(providers))
	for _, provider := range providers {
		ids = append(ids, provider.ID)
	}
	if len(providers) != 2 || ids[0] != codex.ProviderID || ids[1] != grok.ProviderID {
		t.Fatalf("All() with claude and deepinfra disabled = %#v", ids)
	}
}

func TestAllDropsCodexLiveSourceWhenConfiguredOff(t *testing.T) {
	cfg := enabledConfig()
	codexSetting := cfg.Providers[codex.ProviderID]
	codexSetting.Live = "off"
	cfg.Providers[codex.ProviderID] = codexSetting

	providers := registry.All(registry.Options{Config: cfg})
	codexProvider := providers[1]
	if codexProvider.ID != codex.ProviderID {
		t.Fatalf("provider[1] = %#v, want codex", codexProvider)
	}
	if codexProvider.Live != nil {
		t.Fatalf("Codex live = %T, want nil when live is off", codexProvider.Live)
	}
	if codexProvider.Local == nil {
		t.Fatalf("Codex local = nil, want the rollout reader to remain")
	}
}

func TestAllSelectsHTTPForAnExplicitCodexUsageURL(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	const endpoint = "https://example.test/usage"

	cfg := enabledConfig()
	codexSetting := cfg.Providers[codex.ProviderID]
	codexSetting.Live = "http"
	cfg.Providers[codex.ProviderID] = codexSetting

	providers := registry.All(registry.Options{Config: cfg, Env: func(name string) string {
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

func TestAllUsesDeepInfraConfigKeyOverTheEnvironment(t *testing.T) {
	cfg := enabledConfig()
	deepInfraSetting := cfg.Providers[deepinfra.ProviderID]
	deepInfraSetting.APIKey = "from-config"
	cfg.Providers[deepinfra.ProviderID] = deepInfraSetting

	providers := registry.All(registry.Options{
		Config: cfg,
		Env: func(name string) string {
			if name == "DEEPINFRA_KEY" {
				return "from-env"
			}
			return ""
		},
	})
	live, ok := providers[3].Live.(deepinfra.LiveSource)
	if !ok {
		t.Fatalf("DeepInfra live = %T, want LiveSource", providers[3].Live)
	}
	if key := live.Key(); key != "from-config" {
		t.Fatalf("DeepInfra Key() = %q, want the config key to beat the environment", key)
	}
}

func TestAllFallsBackToTheEnvironmentWhenDeepInfraConfigHasNoKey(t *testing.T) {
	providers := registry.All(registry.Options{
		Config: enabledConfig(),
		Env: func(name string) string {
			if name == "DEEPINFRA_KEY" {
				return "from-env"
			}
			return ""
		},
	})
	live, ok := providers[3].Live.(deepinfra.LiveSource)
	if !ok {
		t.Fatalf("DeepInfra live = %T, want LiveSource", providers[3].Live)
	}
	if key := live.Key(); key != "from-env" {
		t.Fatalf("DeepInfra Key() = %q, want the environment fallback", key)
	}
}
