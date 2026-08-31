package codex

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"quotamon/internal/jsonx"
	"quotamon/internal/snapshot"
	"quotamon/internal/source"
)

// HTTPSource reads live usage from an explicitly configured ChatGPT endpoint.
type HTTPSource struct {
	// Home is the Codex CLI data directory containing auth.json.
	Home string
	// Endpoint is the opt-in usage URL.
	Endpoint string
	// Client performs the request and defaults to http.DefaultClient.
	Client *http.Client
}

// ProviderID returns the stable ChatGPT provider identifier.
func (HTTPSource) ProviderID() string { return ProviderID }

// DisplayName returns the provider name shown to users.
func (HTTPSource) DisplayName() string { return DisplayName }

// Origin identifies HTTP readings as live.
func (HTTPSource) Origin() snapshot.Origin { return snapshot.OriginLive }

// Fetch reads credentials, requests the configured endpoint, and normalises usage.
func (s HTTPSource) Fetch(ctx context.Context) (snapshot.Provider, error) {
	home := s.Home
	if home == "" {
		home = DefaultHome()
	}
	authPath := filepath.Join(home, "auth.json")
	authData, err := os.ReadFile(authPath)
	if err != nil {
		if os.IsNotExist(err) {
			return snapshot.Provider{}, source.Errorf(source.NotConfigured, "Sign in with `codex login` — no ChatGPT token in %s", authPath)
		}
		return snapshot.Provider{}, source.Errorf(source.Transport, "Read ChatGPT credentials at %s: %v", authPath, err)
	}
	auth, err := jsonx.Parse(authData)
	if err != nil {
		return snapshot.Provider{}, source.Errorf(source.NotConfigured, "Sign in with `codex login` — no ChatGPT token in %s", authPath)
	}
	tokenValue, ok := jsonx.Get(auth, "tokens", "access_token")
	if !ok {
		return snapshot.Provider{}, source.Errorf(source.NotConfigured, "Sign in with `codex login` — no ChatGPT token in %s", authPath)
	}
	token, ok := jsonx.String(tokenValue)
	if !ok || token == "" {
		return snapshot.Provider{}, source.Errorf(source.NotConfigured, "Sign in with `codex login` — no ChatGPT token in %s", authPath)
	}

	endpoint, err := url.Parse(s.Endpoint)
	if err != nil || endpoint.Scheme == "" || endpoint.Host == "" {
		return snapshot.Provider{}, source.Errorf(source.NotConfigured, "Set QUOTA_MONITOR_CODEX_USAGE_URL to a valid ChatGPT usage endpoint")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return snapshot.Provider{}, source.Errorf(source.Transport, "Create ChatGPT usage request: %v", err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "quotamon/0.1")
	if value, found := jsonx.Get(auth, "tokens", "account_id"); found {
		if accountID, valid := jsonx.String(value); valid && accountID != "" {
			request.Header.Set("ChatGPT-Account-Id", accountID)
		}
	}

	client := s.Client
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		return snapshot.Provider{}, source.Errorf(source.Transport, "ChatGPT usage request failed: %v", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return snapshot.Provider{}, source.Errorf(source.Transport, "Read ChatGPT usage response: %v", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return snapshot.Provider{}, source.ForHTTP(response.StatusCode, DisplayName)
	}

	root, err := jsonx.Parse(body)
	if err != nil {
		return snapshot.Provider{}, source.Errorf(source.Malformed, "Unrecognised response from ChatGPT usage endpoint")
	}
	limits := httpLimits(root)
	provider, ok := Snapshot(limits, time.Now(), snapshot.OriginLive)
	if !ok {
		return snapshot.Provider{}, source.Errorf(source.Malformed, "Unrecognised response from ChatGPT usage endpoint")
	}
	return provider, nil
}

func httpLimits(root any) any {
	if primary, ok := jsonx.Get(root, "rate_limit", "primary_window"); ok {
		limits := map[string]any{"primary": whamWindow(primary)}
		if secondary, found := jsonx.Get(root, "rate_limit", "secondary_window"); found {
			limits["secondary"] = whamWindow(secondary)
		}
		for _, key := range []string{"credits", "plan_type", "planType"} {
			if value, found := jsonx.Get(root, key); found {
				limits[key] = value
			}
		}
		return limits
	}
	if limits, ok := jsonx.Get(root, "rate_limits"); ok {
		return limits
	}
	return root
}

func whamWindow(node any) map[string]any {
	window := make(map[string]any)
	if value, ok := jsonx.Get(node, "used_percent"); ok {
		window["used_percent"] = value
	}
	// The wham endpoint spells the reset `reset_at`; the shared normaliser reads
	// `resets_at`/`resetsAt`. Copying it under the endpoint's own spelling shipped
	// once and silently dropped every live reset time.
	if value, ok := jsonx.Get(node, "reset_at"); ok {
		window["resets_at"] = value
	}
	if value, ok := jsonx.Get(node, "limit_window_seconds"); ok {
		if seconds, valid := jsonx.Float(value); valid {
			window["window_minutes"] = seconds / 60
		}
	}
	return window
}
