package runinfra

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

// defaultBaseURL is RunInfra's API origin; the credits path sits under /v1.
const defaultBaseURL = "https://api.runinfra.ai"

const defaultCallTimeout = 12 * time.Second

// keyEnv is the environment fallback for the RunInfra API key. The registry
// injects a config-backed key first and never parses the repo's masked .env.
const keyEnv = "RUNINFRA_TOKEN"

// LiveSource reads RunInfra prepaid credits and monthly spend from its API.
type LiveSource struct {
	// BaseURL is the RunInfra API origin and defaults to https://api.runinfra.ai.
	BaseURL string
	// Client performs the HTTP requests and defaults to a client with a 15-second timeout.
	Client *http.Client
	// CallTimeout limits each API request and defaults to 12 seconds.
	CallTimeout time.Duration
	// Key returns the RunInfra API key and defaults to reading RUNINFRA_TOKEN from the environment.
	Key func() string
}

// ProviderID returns RunInfra's stable provider identifier.
func (LiveSource) ProviderID() string { return ProviderID }

// DisplayName returns RunInfra's human-readable provider name.
func (LiveSource) DisplayName() string { return DisplayName }

// Origin identifies this source as live.
func (LiveSource) Origin() snapshot.Origin { return snapshot.OriginLive }

// Fetch queries RunInfra's credits endpoint. An empty key is a setup problem,
// not a network failure, so it is reported as NotConfigured without any HTTP
// call. A missing or malformed available_cents or period.spent_cents is an
// unusable reading — never zero spend or zero headroom (T-0051 rule).
func (s LiveSource) Fetch(ctx context.Context) (snapshot.Provider, error) {
	keyProvider := s.Key
	if keyProvider == nil {
		keyProvider = func() string { return os.Getenv(keyEnv) }
	}
	key := keyProvider()
	if key == "" {
		return snapshot.Provider{}, source.Errorf(source.NotConfigured, "Set RUNINFRA_TOKEN or run: quotamon config set runinfra --api-key-stdin to read credits")
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

	// available_cents is the headroom admission checks; a missing or malformed
	// value is an unusable reading, never zero headroom.
	availableValue, found := jsonx.Get(root, "available_cents")
	if !found {
		return snapshot.Provider{}, source.Errorf(source.Malformed, "Unrecognised response from RunInfra credits endpoint")
	}
	availableCents, valid := jsonx.Int(availableValue)
	if !valid {
		return snapshot.Provider{}, source.Errorf(source.Malformed, "Unrecognised response from RunInfra credits endpoint")
	}

	// period.spent_cents is the month-to-date spend; a missing or malformed
	// value is an unusable reading, never zero spend (T-0051 rule).
	spentValue, found := jsonx.Get(root, "period", "spent_cents")
	if !found {
		return snapshot.Provider{}, source.Errorf(source.Malformed, "Unrecognised response from RunInfra credits endpoint")
	}
	spentCents, valid := jsonx.Int(spentValue)
	if !valid {
		return snapshot.Provider{}, source.Errorf(source.Malformed, "Unrecognised response from RunInfra credits endpoint")
	}

	plan := ""
	if value, found := jsonx.Get(root, "plan_tier"); found {
		if text, ok := jsonx.String(value); ok {
			plan = text
		}
	}

	observedAt := time.Now()
	if value, found := jsonx.Get(root, "as_of"); found {
		// as_of carries fractional seconds + Z; jsonx.Time accepts RFC 3339Nano,
		// while our own emitted ObservedAt stays fractional-free per the contract.
		if parsed, ok := jsonx.Time(value); ok {
			observedAt = parsed
		}
	}

	// A hard cap creates the percentage window; a soft or absent cap does not
	// (gates_inference is false, so a soft cap is advisory and must not read as
	// quota pressure). The API reports no period end, so a cap window never has
	// a reset time.
	hasHardCap := false
	capUsedPercent := 0.0
	if limitValue, found := jsonx.Get(root, "spend_cap", "limit_cents"); found {
		if limitCents, ok := jsonx.Int(limitValue); ok && limitCents > 0 {
			if hardValue, found := jsonx.Get(root, "spend_cap", "hard"); found {
				if hard, ok := jsonx.Bool(hardValue); ok && hard {
					if usedValue, found := jsonx.Get(root, "spend_cap", "used_cents"); found {
						if usedCents, ok := jsonx.Int(usedValue); ok {
							hasHardCap = true
							capUsedPercent = float64(usedCents) / float64(limitCents) * 100
						}
					}
				}
			}
		}
	}

	return Snapshot(plan, availableCents, spentCents, capUsedPercent, hasHardCap, observedAt), nil
}

// fetchJSON performs one Authenticated GET and returns the decoded JSON body.
func (s LiveSource) fetchJSON(ctx context.Context, baseURL string, client *http.Client, key string, callTimeout time.Duration) (any, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/v1/credits", nil)
	if err != nil {
		return nil, source.Errorf(source.Transport, "Could not create RunInfra request: %v", err)
	}
	request.Header.Set("Authorization", "Bearer "+key)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "quotamon/0.1")

	response, err := client.Do(request)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, source.Errorf(source.Transport, "RunInfra is slow to answer (>%g s) — will retry next refresh", callTimeout.Seconds())
		}
		return nil, source.Errorf(source.Transport, "Could not reach RunInfra credits endpoint: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		return nil, source.Errorf(source.Unauthorized, "RunInfra API key rejected — set RUNINFRA_TOKEN or run: quotamon config set runinfra --api-key-stdin")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, source.ForHTTP(response.StatusCode, DisplayName)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, source.Errorf(source.Transport, "Could not read RunInfra response: %v", err)
	}
	root, err := jsonx.Parse(body)
	if err != nil {
		return nil, source.Errorf(source.Malformed, "Unrecognised response from RunInfra credits endpoint")
	}
	return root, nil
}
