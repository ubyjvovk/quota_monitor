package deepinfra

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

// defaultBaseURL is DeepInfra's payment API origin. The payment paths are, by
// design, NOT under /v1 — see PROVIDERS.md.
const defaultBaseURL = "https://api.deepinfra.com"

// keyEnv is the environment variable holding the DeepInfra API key. It is read
// from the environment only; the repo's .env is masked from workers and the
// code must never learn to parse it.
const keyEnv = "DEEPINFRA_KEY"

// LiveSource reads DeepInfra month-to-date spend from its payment endpoints.
type LiveSource struct {
	// BaseURL is the DeepInfra payment API origin and defaults to https://api.deepinfra.com.
	BaseURL string
	// Client performs the HTTP requests and defaults to a client with a 15-second timeout.
	Client *http.Client
	// Key returns the DeepInfra API key and defaults to reading DEEPINFRA_KEY from the environment.
	Key func() string
}

// ProviderID returns DeepInfra's stable provider identifier.
func (LiveSource) ProviderID() string { return ProviderID }

// DisplayName returns DeepInfra's human-readable provider name.
func (LiveSource) DisplayName() string { return DisplayName }

// Origin identifies this source as live.
func (LiveSource) Origin() snapshot.Origin { return snapshot.OriginLive }

// Fetch queries DeepInfra's spending-limit config and month-to-date usage, then
// normalises them into a provider reading. An empty key is a setup problem, not
// a network failure, so it is reported as NotConfigured without any HTTP call.
func (s LiveSource) Fetch(ctx context.Context) (snapshot.Provider, error) {
	keyProvider := s.Key
	if keyProvider == nil {
		keyProvider = func() string { return os.Getenv(keyEnv) }
	}
	key := keyProvider()
	if key == "" {
		return snapshot.Provider{}, source.Errorf(source.NotConfigured, "Set DEEPINFRA_KEY to read DeepInfra spend")
	}

	baseURL := s.BaseURL
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	client := s.Client
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}

	config, err := s.fetchJSON(ctx, baseURL, client, key, "/payment/config")
	if err != nil {
		return snapshot.Provider{}, err
	}
	// A missing or absent limit means no spending ceiling, never a conversation
	// into a percentage.
	limitUSD := -1.0
	if value, found := jsonx.Get(config, "limit"); found {
		if limit, valid := jsonx.Float(value); valid {
			limitUSD = limit
		}
	}

	usage, err := s.fetchJSON(ctx, baseURL, client, key, "/payment/usage?from=current")
	if err != nil {
		return snapshot.Provider{}, err
	}
	months, found := jsonx.Get(usage, "months")
	if !found {
		return snapshot.Provider{}, source.Errorf(source.Malformed, "Unrecognised response from DeepInfra usage endpoint")
	}
	list, ok := months.([]any)
	if !ok || len(list) == 0 {
		return snapshot.Provider{}, source.Errorf(source.Malformed, "Unrecognised response from DeepInfra usage endpoint")
	}
	month := list[0]

	spentUSD := 0.0
	if value, found := jsonx.Get(month, "total_cost"); found {
		// total_cost is in cents; spend is reported in whole USD.
		if cents, valid := jsonx.Float(value); valid {
			spentUSD = cents / 100
		}
	}

	var periodEnd *time.Time
	if value, found := jsonx.Get(month, "interval", "to"); found {
		// interval is epoch milliseconds; jsonx.Time coerces it to a timestamp.
		if parsed, valid := jsonx.Time(value); valid {
			periodEnd = &parsed
		}
	}

	return Snapshot(limitUSD, limitUSD > 0, spentUSD, periodEnd, time.Now()), nil
}

// fetchJSON performs one Authenticated GET and returns the decoded JSON body.
func (s LiveSource) fetchJSON(ctx context.Context, baseURL string, client *http.Client, key, path string) (any, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+path, nil)
	if err != nil {
		return nil, source.Errorf(source.Transport, "Could not create DeepInfra request: %v", err)
	}
	request.Header.Set("Authorization", "Bearer "+key)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "quotamon/0.1")

	response, err := client.Do(request)
	if err != nil {
		return nil, source.Errorf(source.Transport, "Could not reach DeepInfra payment endpoint: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		return nil, source.Errorf(source.Unauthorized, "DeepInfra rejected DEEPINFRA_KEY")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, source.ForHTTP(response.StatusCode, DisplayName)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, source.Errorf(source.Transport, "Could not read DeepInfra response: %v", err)
	}
	root, err := jsonx.Parse(body)
	if err != nil {
		return nil, source.Errorf(source.Malformed, "Unrecognised response from DeepInfra payment endpoint")
	}
	return root, nil
}
