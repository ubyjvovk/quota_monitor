package openrouter

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"quotamon/internal/jsonx"
	"quotamon/internal/snapshot"
	"quotamon/internal/source"
)

const defaultBaseURL = "https://openrouter.ai"

const defaultCallTimeout = 12 * time.Second

// keyEnv is the environment fallback for the OpenRouter API key. The registry
// injects a config-backed key first.
const keyEnv = "OPENROUTER_KEY"

// LiveSource reads OpenRouter prepaid credits and lifetime usage from its API.
type LiveSource struct {
	// BaseURL is the OpenRouter origin and defaults to https://openrouter.ai.
	BaseURL string
	// Client performs the HTTP request and defaults to a client with a 15-second timeout.
	Client *http.Client
	// CallTimeout limits the API request and defaults to 12 seconds.
	CallTimeout time.Duration
	// Key returns the OpenRouter API key and defaults to OPENROUTER_KEY.
	Key func() string
}

// ProviderID returns OpenRouter's stable provider identifier.
func (LiveSource) ProviderID() string { return ProviderID }

// DisplayName returns OpenRouter's human-readable provider name.
func (LiveSource) DisplayName() string { return DisplayName }

// Origin identifies this source as live.
func (LiveSource) Origin() snapshot.Origin { return snapshot.OriginLive }

// Fetch queries OpenRouter's credits endpoint. Missing or non-numeric totals
// are malformed readings, never fabricated zero money.
func (s LiveSource) Fetch(ctx context.Context) (snapshot.Provider, error) {
	keyProvider := s.Key
	if keyProvider == nil {
		keyProvider = func() string { return os.Getenv(keyEnv) }
	}
	key := keyProvider()
	if key == "" {
		return snapshot.Provider{}, source.Errorf(source.NotConfigured, "Set OPENROUTER_KEY or run: quotamon config set openrouter --api-key-stdin to read credits")
	}

	baseURL := s.BaseURL
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	client := s.Client
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	callTimeout := s.CallTimeout
	if callTimeout <= 0 {
		callTimeout = defaultCallTimeout
	}

	callContext, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	root, err := s.fetchJSON(callContext, baseURL, client, key, callTimeout)
	if err != nil {
		return snapshot.Provider{}, err
	}

	totalCreditsValue, found := jsonx.Get(root, "data", "total_credits")
	if !found {
		return snapshot.Provider{}, malformedError()
	}
	totalCredits, valid := jsonx.Float(totalCreditsValue)
	if !valid {
		return snapshot.Provider{}, malformedError()
	}
	totalUsageValue, found := jsonx.Get(root, "data", "total_usage")
	if !found {
		return snapshot.Provider{}, malformedError()
	}
	totalUsage, valid := jsonx.Float(totalUsageValue)
	if !valid {
		return snapshot.Provider{}, malformedError()
	}

	return Snapshot(totalCredits, totalUsage, time.Now()), nil
}

func (s LiveSource) fetchJSON(ctx context.Context, baseURL string, client *http.Client, key string, callTimeout time.Duration) (any, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/api/v1/credits", nil)
	if err != nil {
		return nil, source.Errorf(source.Transport, "Could not create OpenRouter request: %v", err)
	}
	request.Header.Set("Authorization", "Bearer "+key)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "quotamon/0.1")

	response, err := client.Do(request)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, source.Errorf(source.Transport, "OpenRouter is slow to answer (>%g s) — will retry next refresh", callTimeout.Seconds())
		}
		return nil, source.Errorf(source.Transport, "Could not reach OpenRouter credits endpoint: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		return nil, source.Errorf(source.Unauthorized, "OpenRouter API key rejected — set OPENROUTER_KEY or run: quotamon config set openrouter --api-key-stdin")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, source.ForHTTP(response.StatusCode, DisplayName)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, source.Errorf(source.Transport, "Could not read OpenRouter response: %v", err)
	}
	root, err := jsonx.Parse(body)
	if err != nil {
		return nil, malformedError()
	}
	return root, nil
}

func malformedError() *source.Error {
	return source.Errorf(source.Malformed, "Unrecognised response from OpenRouter credits endpoint")
}
