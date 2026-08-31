package registry_test

import (
	"path/filepath"
	"testing"
	"time"

	"quotamon/internal/config"
	"quotamon/internal/providers/claude"
	"quotamon/internal/providers/codex"
	"quotamon/internal/providers/deepinfra"
	"quotamon/internal/providers/deepseek"
	"quotamon/internal/providers/grok"
	"quotamon/internal/providers/kimi"
	"quotamon/internal/providers/openrouter"
	"quotamon/internal/providers/runinfra"
	"quotamon/internal/registry"
)

// enabledConfig returns a configuration that turns every known provider on,
// leaving each provider's provider-specific settings at their defaults.
func enabledConfig() config.Config {
	providers := map[string]config.Provider{}
	for _, id := range []string{claude.ProviderID, codex.ProviderID, grok.ProviderID, deepinfra.ProviderID, kimi.ProviderID, runinfra.ProviderID, openrouter.ProviderID, deepseek.ProviderID} {
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

	if len(providers) != 8 || providers[0].ID != claude.ProviderID || providers[1].ID != codex.ProviderID || providers[2].ID != grok.ProviderID || providers[3].ID != deepinfra.ProviderID || providers[4].ID != kimi.ProviderID || providers[5].ID != runinfra.ProviderID || providers[6].ID != openrouter.ProviderID || providers[7].ID != deepseek.ProviderID {
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
	if providers[4].Local != nil {
		t.Fatalf("Kimi local = %T, want nil", providers[4].Local)
	}
	if _, ok := providers[4].Live.(kimi.LiveSource); !ok {
		t.Fatalf("Kimi live = %T, want LiveSource", providers[4].Live)
	}
	if providers[5].Local != nil {
		t.Fatalf("RunInfra local = %T, want nil", providers[5].Local)
	}
	if _, ok := providers[5].Live.(runinfra.LiveSource); !ok {
		t.Fatalf("RunInfra live = %T, want LiveSource", providers[5].Live)
	}
	if providers[6].Local != nil {
		t.Fatalf("OpenRouter local = %T, want nil", providers[6].Local)
	}
	if _, ok := providers[6].Live.(openrouter.LiveSource); !ok {
		t.Fatalf("OpenRouter live = %T, want LiveSource", providers[6].Live)
	}
	if providers[7].Local != nil {
		t.Fatalf("DeepSeek local = %T, want nil", providers[7].Local)
	}
	if _, ok := providers[7].Live.(deepseek.LiveSource); !ok {
		t.Fatalf("DeepSeek live = %T, want LiveSource", providers[7].Live)
	}
	wantWindows := []time.Duration{5 * time.Hour, 5 * time.Hour, 7 * 24 * time.Hour, 30 * 24 * time.Hour, 5 * time.Hour, 30 * 24 * time.Hour, 30 * 24 * time.Hour, 30 * 24 * time.Hour}
	for index, provider := range providers {
		if provider.Cache == nil || provider.ShortestWindow != wantWindows[index] {
			t.Errorf("provider %s cache/window = %v/%s, want configured/%s", provider.ID, provider.Cache, provider.ShortestWindow, wantWindows[index])
		}
	}
	if providers[0].TokenStale != nil || providers[1].TokenStale != nil || providers[2].TokenStale == nil || providers[3].TokenStale != nil || providers[4].TokenStale == nil || providers[5].TokenStale != nil || providers[6].TokenStale != nil || providers[7].TokenStale != nil {
		t.Fatalf("token-stale policies are not wired only for Grok and Kimi")
	}
	if providers[0].Refresh != nil || providers[1].Refresh != nil || providers[2].Refresh != nil || providers[3].Refresh != nil || providers[4].Refresh == nil || providers[5].Refresh != nil || providers[6].Refresh != nil || providers[7].Refresh != nil {
		t.Fatalf("refresh policy is not wired only for Kimi")
	}
}

func TestAllAppliesLivePolicyAcrossEnabledProviders(t *testing.T) {
	providers := registry.All(registry.Options{
		Config:      enabledConfig(),
		LiveEnabled: func(id string) bool { return id != claude.ProviderID },
	})
	if providers[0].LiveEnabled {
		t.Fatal("All() live policy left Claude enabled")
	}
	for _, provider := range providers[1:] {
		if !provider.LiveEnabled {
			t.Errorf("All() live policy disabled %s, want only Claude disabled", provider.ID)
		}
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
	if len(providers) != 6 || ids[0] != codex.ProviderID || ids[1] != grok.ProviderID || ids[2] != kimi.ProviderID || ids[3] != runinfra.ProviderID || ids[4] != openrouter.ProviderID || ids[5] != deepseek.ProviderID {
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

func TestAllUsesRunInfraConfigKeyOverTheEnvironment(t *testing.T) {
	cfg := enabledConfig()
	runInfraSetting := cfg.Providers[runinfra.ProviderID]
	runInfraSetting.APIKey = "from-config"
	cfg.Providers[runinfra.ProviderID] = runInfraSetting

	providers := registry.All(registry.Options{
		Config: cfg,
		Env: func(name string) string {
			if name == "RUNINFRA_TOKEN" {
				return "from-env"
			}
			return ""
		},
	})
	live, ok := providers[5].Live.(runinfra.LiveSource)
	if !ok {
		t.Fatalf("RunInfra live = %T, want LiveSource", providers[5].Live)
	}
	if key := live.Key(); key != "from-config" {
		t.Fatalf("RunInfra Key() = %q, want the config key to beat the environment", key)
	}
}

func TestAllFallsBackToTheEnvironmentWhenRunInfraConfigHasNoKey(t *testing.T) {
	providers := registry.All(registry.Options{
		Config: enabledConfig(),
		Env: func(name string) string {
			if name == "RUNINFRA_TOKEN" {
				return "from-env"
			}
			return ""
		},
	})
	live, ok := providers[5].Live.(runinfra.LiveSource)
	if !ok {
		t.Fatalf("RunInfra live = %T, want LiveSource", providers[5].Live)
	}
	if key := live.Key(); key != "from-env" {
		t.Fatalf("RunInfra Key() = %q, want the environment fallback", key)
	}
}

func TestAllUsesOpenRouterConfigKeyOverTheEnvironment(t *testing.T) {
	cfg := enabledConfig()
	setting := cfg.Providers[openrouter.ProviderID]
	setting.APIKey = "from-config"
	cfg.Providers[openrouter.ProviderID] = setting

	providers := registry.All(registry.Options{
		Config: cfg,
		Env: func(name string) string {
			if name == "OPENROUTER_KEY" {
				return "from-env"
			}
			return ""
		},
	})
	live, ok := providers[6].Live.(openrouter.LiveSource)
	if !ok || live.Key() != "from-config" {
		t.Fatalf("OpenRouter live = %#v, want config-backed LiveSource", providers[6].Live)
	}
}

func TestAllFallsBackToTheEnvironmentWhenOpenRouterConfigHasNoKey(t *testing.T) {
	providers := registry.All(registry.Options{
		Config: enabledConfig(),
		Env: func(name string) string {
			if name == "OPENROUTER_KEY" {
				return "from-env"
			}
			return ""
		},
	})
	live, ok := providers[6].Live.(openrouter.LiveSource)
	if !ok || live.Key() != "from-env" {
		t.Fatalf("OpenRouter live = %#v, want environment-backed LiveSource", providers[6].Live)
	}
}

func TestAllUsesDeepSeekConfigKeyOverTheEnvironment(t *testing.T) {
	cfg := enabledConfig()
	setting := cfg.Providers[deepseek.ProviderID]
	setting.APIKey = "from-config"
	cfg.Providers[deepseek.ProviderID] = setting

	providers := registry.All(registry.Options{
		Config: cfg,
		Env: func(name string) string {
			if name == "DEEPSEEK_KEY" {
				return "from-env"
			}
			return ""
		},
	})
	live, ok := providers[7].Live.(deepseek.LiveSource)
	if !ok || live.Key() != "from-config" {
		t.Fatalf("DeepSeek live = %#v, want config-backed LiveSource", providers[7].Live)
	}
}

func TestAllFallsBackToTheEnvironmentWhenDeepSeekConfigHasNoKey(t *testing.T) {
	providers := registry.All(registry.Options{
		Config: enabledConfig(),
		Env: func(name string) string {
			if name == "DEEPSEEK_KEY" {
				return "from-env"
			}
			return ""
		},
	})
	live, ok := providers[7].Live.(deepseek.LiveSource)
	if !ok || live.Key() != "from-env" {
		t.Fatalf("DeepSeek live = %#v, want environment-backed LiveSource", providers[7].Live)
	}
}

func TestAllPropagatesFreshToEveryEnabledProvider(t *testing.T) {
	providers := registry.All(registry.Options{Config: enabledConfig(), Fresh: true})
	for _, provider := range providers {
		if !provider.Fresh {
			t.Errorf("provider %s Fresh = false, want true", provider.ID)
		}
	}
}
