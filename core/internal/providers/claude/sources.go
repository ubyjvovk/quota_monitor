package claude

import (
	"context"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"quotamon/internal/jsonx"
	"quotamon/internal/snapshot"
	"quotamon/internal/source"
)

const defaultBaseURL = "https://api.anthropic.com"

// LocalSource reads Claude usage from the statusline mirror.
type LocalSource struct {
	// MirrorPath is the path to the statusline mirror JSON file.
	MirrorPath string
}

// ProviderID returns Claude's stable provider identifier.
func (LocalSource) ProviderID() string { return ProviderID }

// DisplayName returns Claude's human-readable provider name.
func (LocalSource) DisplayName() string { return DisplayName }

// Origin identifies this source as local.
func (LocalSource) Origin() snapshot.Origin { return snapshot.OriginLocal }

// Fetch reads and normalises the Claude statusline mirror.
func (s LocalSource) Fetch(_ context.Context) (snapshot.Provider, error) {
	path := s.MirrorPath
	if path == "" {
		path = DefaultMirrorPath()
	}
	body, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return snapshot.Provider{}, source.Errorf(source.NotConfigured, "Statusline mirror not installed — run install-claude-statusline.sh")
		}
		return snapshot.Provider{}, source.Errorf(source.Transport, "Could not read Claude statusline mirror: %v", err)
	}
	root, err := jsonx.Parse(body)
	if err != nil {
		return snapshot.Provider{}, source.Errorf(source.Malformed, "Claude statusline mirror contains invalid JSON")
	}

	observedAt := time.Time{}
	if value, ok := first(root, "observed_at", "observedAt"); ok {
		observedAt, _ = jsonx.Time(value)
	}
	if observedAt.IsZero() {
		info, err := os.Stat(path)
		if err != nil {
			return snapshot.Provider{}, source.Errorf(source.Transport, "Could not inspect Claude statusline mirror: %v", err)
		}
		observedAt = info.ModTime()
	}

	limits := root
	if value, ok := jsonx.Get(root, "rate_limits"); ok {
		limits = value
	}
	plan := ""
	if value, ok := first(root, "subscription_type", "plan"); ok {
		plan, _ = jsonx.String(value)
	}
	provider, ok := Snapshot(limits, observedAt, snapshot.OriginLocal, plan)
	if !ok {
		return snapshot.Provider{}, source.Errorf(source.NoDataFound, "Mirror has no usage yet — Claude Code populates it after its first reply")
	}
	return provider, nil
}

// LiveSource reads Claude usage from Anthropic's OAuth endpoint.
type LiveSource struct {
	// BaseURL is the Anthropic API origin and defaults to https://api.anthropic.com.
	BaseURL string
	// Client performs the HTTP request and defaults to a client with a 15-second timeout.
	Client *http.Client
	// Credentials returns raw Claude credential JSON and defaults to ReadCredentials.
	Credentials func() ([]byte, error)
}

// ProviderID returns Claude's stable provider identifier.
func (LiveSource) ProviderID() string { return ProviderID }

// DisplayName returns Claude's human-readable provider name.
func (LiveSource) DisplayName() string { return DisplayName }

// Origin identifies this source as live.
func (LiveSource) Origin() snapshot.Origin { return snapshot.OriginLive }

// Fetch queries and normalises Anthropic's Claude usage endpoint.
func (s LiveSource) Fetch(ctx context.Context) (snapshot.Provider, error) {
	credentialsProvider := s.Credentials
	if credentialsProvider == nil {
		credentialsProvider = ReadCredentials
	}
	blob, err := credentialsProvider()
	if err != nil {
		return snapshot.Provider{}, err
	}
	credentials, err := ParseCredentials(blob)
	if err != nil {
		return snapshot.Provider{}, err
	}

	baseURL := s.BaseURL
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/api/oauth/usage", nil)
	if err != nil {
		return snapshot.Provider{}, source.Errorf(source.Transport, "Could not create Claude usage request: %v", err)
	}
	request.Header.Set("Authorization", "Bearer "+credentials.Token)
	request.Header.Set("anthropic-beta", "oauth-2025-04-20")
	request.Header.Set("anthropic-version", "2023-06-01")
	request.Header.Set("Accept", "application/json")
	// Spoofing Claude Code's identity did not change endpoint behaviour, so the client identifies honestly.
	request.Header.Set("User-Agent", "quotamon/0.1")

	client := s.Client
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	response, err := client.Do(request)
	if err != nil {
		return snapshot.Provider{}, source.Errorf(source.Transport, "Could not reach Claude usage endpoint: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		// Claude Code refreshes lazily, so an old local timestamp is only a hint
		// that shapes a genuine rejection; it must never prevent the request.
		looksExpired := credentials.ExpiresAt != nil && !credentials.ExpiresAt.After(time.Now())
		if looksExpired {
			return snapshot.Provider{}, source.Errorf(source.Unauthorized, "Claude sign-in expired — run `claude` in a terminal to refresh it")
		}
		return snapshot.Provider{}, source.Errorf(source.Unauthorized, "Claude rejected the token — run `claude` in a terminal to sign in again")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return snapshot.Provider{}, source.ForHTTP(response.StatusCode, DisplayName)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return snapshot.Provider{}, source.Errorf(source.Transport, "Could not read Claude usage response: %v", err)
	}
	root, err := jsonx.Parse(body)
	if err != nil {
		return snapshot.Provider{}, source.Errorf(source.Malformed, "Unrecognised response from Claude usage endpoint")
	}
	limits := root
	if value, ok := jsonx.Get(root, "rate_limits"); ok {
		limits = value
	}
	provider, ok := Snapshot(limits, time.Now(), snapshot.OriginLive, credentials.Plan)
	if !ok {
		return snapshot.Provider{}, source.Errorf(source.Malformed, "Unrecognised response from Claude usage endpoint")
	}
	return provider, nil
}
