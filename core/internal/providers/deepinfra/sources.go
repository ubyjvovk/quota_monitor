package deepinfra

import (
	"context"
	"errors"
	"fmt"
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

const defaultCallTimeout = 8 * time.Second

// keyEnv is the environment fallback for the DeepInfra API key. The registry
// injects a config-backed key first and never parses the repo's masked .env.
const keyEnv = "DEEPINFRA_KEY"

// LiveSource reads DeepInfra month-to-date spend from its payment endpoints.
type LiveSource struct {
	// BaseURL is the DeepInfra payment API origin and defaults to https://api.deepinfra.com.
	BaseURL string
	// Client performs the HTTP requests and defaults to a client with a 15-second timeout.
	Client *http.Client
	// CallTimeout limits each payment API request independently and defaults to 8 seconds.
	CallTimeout time.Duration
	// Key returns the DeepInfra API key and defaults to reading DEEPINFRA_KEY from the environment.
	Key func() string
}

// ProviderID returns DeepInfra's stable provider identifier.
func (LiveSource) ProviderID() string { return ProviderID }

// DisplayName returns DeepInfra's human-readable provider name.
func (LiveSource) DisplayName() string { return DisplayName }

// Origin identifies this source as live.
func (LiveSource) Origin() snapshot.Origin { return snapshot.OriginLive }

// Fetch queries DeepInfra's spending-limit config and month-to-date usage in
// parallel because either endpoint can otherwise consume most of the caller's
// refresh budget. An empty key is a setup problem, not a network failure, so it
// is reported as NotConfigured without any HTTP call.
func (s LiveSource) Fetch(ctx context.Context) (snapshot.Provider, error) {
	keyProvider := s.Key
	if keyProvider == nil {
		keyProvider = func() string { return os.Getenv(keyEnv) }
	}
	key := keyProvider()
	if key == "" {
		return snapshot.Provider{}, source.Errorf(source.NotConfigured, "Add DeepInfra api_key to config.json or set DEEPINFRA_KEY to read spend")
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

	type result struct {
		value any
		err   error
	}
	configResult := make(chan result, 1)
	usageResult := make(chan result, 1)
	checklistResult := make(chan result, 1)
	fetch := func(path string, output chan<- result) {
		callContext, cancel := context.WithTimeout(ctx, callTimeout)
		defer cancel()
		value, err := s.fetchJSON(callContext, baseURL, client, key, path)
		output <- result{value: value, err: err}
	}
	go fetch("/payment/config", configResult)
	go fetch("/payment/usage?from=current", usageResult)
	go fetch("/payment/checklist", checklistResult)

	configResponse := <-configResult
	usageResponse := <-usageResult
	checklistResponse := <-checklistResult
	if usageResponse.err != nil {
		// Usage is the essential reading. A sibling Unauthorized is more
		// actionable (the key is bad), so surface it over the usage breakdown.
		if isUnauthorized(configResponse.err) {
			return snapshot.Provider{}, configResponse.err
		}
		if isUnauthorized(checklistResponse.err) {
			return snapshot.Provider{}, checklistResponse.err
		}
		return snapshot.Provider{}, usageResponse.err
	}

	config := configResponse.value
	// A missing or absent limit means no spending ceiling, never a conversation
	// into a percentage.
	limitUSD := -1.0
	if configResponse.err == nil {
		if value, found := jsonx.Get(config, "limit"); found {
			if limit, valid := jsonx.Float(value); valid {
				limitUSD = limit
			}
		}
	}

	usage := usageResponse.value
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

	// A checklist failure is not fatal: usage still gives a spend-only reading,
	// and whether the account holds prepaid funds is reported as a setup issue
	// instead.
	balance := Balance{}
	if checklistResponse.err == nil {
		balance = balanceFromChecklist(checklistResponse.value)
	}

	provider := Snapshot(limitUSD, limitUSD > 0, spentUSD, periodEnd, time.Now(), balance)
	if !balance.Suspended {
		// A suspended account already reports a Failed status; neither missing
		// ceiling nor missing balance should hide that. Order matters: the
		// spending-limit message wins when both endpoints are down, matching the
		// pre-balance behaviour.
		if checklistResponse.err != nil {
			provider.Status = snapshot.NeedsSetup(fmt.Sprintf("Balance unavailable — DeepInfra /payment/checklist: %v", checklistResponse.err))
		}
		if configResponse.err != nil {
			provider.Status = snapshot.NeedsSetup(fmt.Sprintf("Spending limit unknown — DeepInfra /payment/config: %v", configResponse.err))
		}
	}
	return provider, nil
}

// balanceFromChecklist extracts only the money/status fields from the response
// body. The same payload also carries the user's postal address and payment
// method; those are deliberately not read and the body is never retained.
func balanceFromChecklist(checklist any) Balance {
	balance := Balance{Known: true}
	if value, found := jsonx.Get(checklist, "stripe_balance"); found {
		if amount, valid := jsonx.Float(value); valid {
			balance.Stripe = amount
		}
	}
	if value, found := jsonx.Get(checklist, "suspended"); found {
		if suspended, valid := jsonx.Bool(value); valid {
			balance.Suspended = suspended
		}
	}
	if value, found := jsonx.Get(checklist, "suspend_reason"); found {
		if reason, valid := jsonx.String(value); valid {
			balance.SuspendReason = reason
		}
	}
	if value, found := jsonx.Get(checklist, "overdue_invoices"); found {
		if overdue, valid := jsonx.Float(value); valid {
			balance.OverdueInvoices = int(overdue)
		}
	}
	return balance
}

func isUnauthorized(err error) bool {
	var sourceError *source.Error
	return errors.As(err, &sourceError) && sourceError.Kind == source.Unauthorized
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
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, source.Errorf(source.Transport, "DeepInfra is slow to answer (>8 s) — will retry next refresh")
		}
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
