// Package registry constructs QuotaMon's providers in stable display order.
package registry

import (
	"os"

	"quotamon/internal/hybrid"
	"quotamon/internal/providers/claude"
	"quotamon/internal/providers/codex"
	"quotamon/internal/source"
)

// Options injects live-source policy and environment access.
type Options struct {
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

	codexHome := codex.DefaultHome()
	var codexLive source.Source = codex.AppServerSource{}
	if endpoint := environment("QUOTA_MONITOR_CODEX_USAGE_URL"); endpoint != "" {
		codexLive = codex.HTTPSource{Home: codexHome, Endpoint: endpoint}
	}

	return []hybrid.Provider{
		{
			ID:          claude.ProviderID,
			DisplayName: claude.DisplayName,
			Local:       claude.LocalSource{MirrorPath: claude.DefaultMirrorPath()},
			Live:        claude.LiveSource{},
			LiveEnabled: liveEnabled(claude.ProviderID),
		},
		{
			ID:          codex.ProviderID,
			DisplayName: codex.DisplayName,
			Local: codex.LocalSource{
				Home:      codexHome,
				MaxFiles:  16,
				TailBytes: 512 * 1024,
			},
			Live:        codexLive,
			LiveEnabled: liveEnabled(codex.ProviderID),
		},
	}
}
