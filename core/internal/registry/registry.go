// Package registry constructs QuotaMon's providers in stable display order.
package registry

import (
	"os"

	"quotamon/internal/config"
	"quotamon/internal/hybrid"
	"quotamon/internal/providers/claude"
	"quotamon/internal/providers/codex"
	"quotamon/internal/providers/deepinfra"
	"quotamon/internal/providers/grok"
	"quotamon/internal/source"
)

// Options injects live-source policy and environment access.
type Options struct {
	// Config selects enabled providers and their provider-specific settings.
	Config config.Config
	// LiveEnabled reports whether a provider's live source may run and defaults to true.
	LiveEnabled func(id string) bool
	// Env reads an environment variable and defaults to os.Getenv.
	Env func(string) string
}

// All returns every supported provider in stable display order.
func All(options Options) []hybrid.Provider {
	liveEnabled := options.LiveEnabled
	if liveEnabled == nil {
		liveEnabled = func(string) bool { return true }
	}
	environment := options.Env
	if environment == nil {
		environment = os.Getenv
	}

	configured := options.Config.Providers
	codexHome := codex.DefaultHome()
	var codexLive source.Source
	switch configured[codex.ProviderID].Live {
	case "off":
		codexLive = nil
	case "http":
		codexLive = codex.HTTPSource{Home: codexHome, Endpoint: environment("QUOTA_MONITOR_CODEX_USAGE_URL")}
	default:
		codexLive = codex.AppServerSource{}
	}

	providers := make([]hybrid.Provider, 0, 4)
	if configured[claude.ProviderID].Enabled {
		providers = append(providers, hybrid.Provider{
			ID:          claude.ProviderID,
			DisplayName: claude.DisplayName,
			Local:       claude.LocalSource{MirrorPath: claude.DefaultMirrorPath()},
			Live:        claude.LiveSource{},
			LiveEnabled: liveEnabled(claude.ProviderID),
		})
	}
	if configured[codex.ProviderID].Enabled {
		providers = append(providers, hybrid.Provider{
			ID:          codex.ProviderID,
			DisplayName: codex.DisplayName,
			Local: codex.LocalSource{
				Home:      codexHome,
				MaxFiles:  16,
				TailBytes: 512 * 1024,
			},
			Live:        codexLive,
			LiveEnabled: liveEnabled(codex.ProviderID),
		})
	}
	if configured[grok.ProviderID].Enabled {
		providers = append(providers, hybrid.Provider{
			ID:          grok.ProviderID,
			DisplayName: grok.DisplayName,
			Local:       nil,
			Live:        grok.LiveSource{},
			LiveEnabled: liveEnabled(grok.ProviderID),
		})
	}
	if configured[deepinfra.ProviderID].Enabled {
		deepInfraConfig := configured[deepinfra.ProviderID]
		providers = append(providers, hybrid.Provider{
			ID:          deepinfra.ProviderID,
			DisplayName: deepinfra.DisplayName,
			Local:       nil,
			Live: deepinfra.LiveSource{Key: func() string {
				if deepInfraConfig.APIKey != "" {
					return deepInfraConfig.APIKey
				}
				return environment("DEEPINFRA_KEY")
			}},
			LiveEnabled: liveEnabled(deepinfra.ProviderID),
		})
	}
	return providers
}
