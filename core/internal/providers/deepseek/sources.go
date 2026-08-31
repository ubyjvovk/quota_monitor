package deepseek

import (
	"context"
	"errors"
	"io"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"quotamon/internal/jsonx"
	"quotamon/internal/snapshot"
	"quotamon/internal/source"
)

const defaultBaseURL = "https://api.deepseek.com"

const defaultCallTimeout = 12 * time.Second

// keyEnv is the environment fallback for the DeepSeek API key. The registry
// injects a config-backed key first.
const keyEnv = "DEEPSEEK_KEY"

// LiveSource reads DeepSeek's current account balance from its API.
type LiveSource struct {
	// BaseURL is the DeepSeek API origin and defaults to https://api.deepseek.com.
	BaseURL string
	// Client performs the HTTP request and defaults to a client with a 15-second timeout.
	Client *http.Client
	// CallTimeout limits the API request and defaults to 12 seconds.
	CallTimeout time.Duration
	// Key returns the DeepSeek API key and defaults to DEEPSEEK_KEY.
	Key func() string
}

// ProviderID returns DeepSeek's stable provider identifier.
func (LiveSource) ProviderID() string { return ProviderID }

// DisplayName returns DeepSeek's human-readable provider name.
func (LiveSource) DisplayName() string { return DisplayName }

// Origin identifies this source as live.
func (LiveSource) Origin() snapshot.Origin { return snapshot.OriginLive }

// Fetch queries DeepSeek's balance endpoint. It prefers a USD balance and
// otherwise uses the first reported currency. An empty balance list or an
// unparsable string amount is malformed, never fabricated zero money.
func (s LiveSource) Fetch(ctx context.Context) (snapshot.Provider, error) {
	keyProvider := s.Key
	if keyProvider == nil {
		keyProvider = func() string { return os.Getenv(keyEnv) }
	}
	key := keyProvider()
	if key == "" {
		return snapshot.Provider{}, source.Errorf(source.NotConfigured, "Set DEEPSEEK_KEY or run: quotamon config set deepseek --api-key-stdin to read balance")
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

	balancesValue, found := jsonx.Get(root, "balance_infos")
	if !found {
		return snapshot.Provider{}, malformedError()
	}
	balances, valid := balancesValue.([]any)
	if !valid || len(balances) == 0 {
		return snapshot.Provider{}, malformedError()
	}
	selected := balances[0]
	for _, balance := range balances {
		if value, found := jsonx.Get(balance, "currency"); found {
			if currency, ok := jsonx.String(value); ok && currency == "USD" {
				selected = balance
				break
			}
		}
	}

	currencyValue, found := jsonx.Get(selected, "currency")
	if !found {
		return snapshot.Provider{}, malformedError()
	}
	currency, valid := jsonx.String(currencyValue)
	if !valid || currency == "" {
		return snapshot.Provider{}, malformedError()
	}
	totalValue, found := jsonx.Get(selected, "total_balance")
	if !found {
		return snapshot.Provider{}, malformedError()
	}
	totalText, valid := jsonx.String(totalValue)
	if !valid {
		return snapshot.Provider{}, malformedError()
	}
	totalBalance, err := strconv.ParseFloat(totalText, 64)
	if err != nil || math.IsNaN(totalBalance) || math.IsInf(totalBalance, 0) {
		return snapshot.Provider{}, malformedError()
	}

	available := false
	if availableValue, found := jsonx.Get(root, "is_available"); found {
		available, _ = jsonx.Bool(availableValue)
	}
	return Snapshot(currency, totalBalance, available, time.Now()), nil
}

func (s LiveSource) fetchJSON(ctx context.Context, baseURL string, client *http.Client, key string, callTimeout time.Duration) (any, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/user/balance", nil)
	if err != nil {
		return nil, source.Errorf(source.Transport, "Could not create DeepSeek request: %v", err)
	}
	request.Header.Set("Authorization", "Bearer "+key)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "quotamon/0.1")

	response, err := client.Do(request)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, source.Errorf(source.Transport, "DeepSeek is slow to answer (>%g s) — will retry next refresh", callTimeout.Seconds())
		}
		return nil, source.Errorf(source.Transport, "Could not reach DeepSeek balance endpoint: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		return nil, source.Errorf(source.Unauthorized, "DeepSeek API key rejected — set DEEPSEEK_KEY or run: quotamon config set deepseek --api-key-stdin")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, source.ForHTTP(response.StatusCode, DisplayName)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, source.Errorf(source.Transport, "Could not read DeepSeek response: %v", err)
	}
	root, err := jsonx.Parse(body)
	if err != nil {
		return nil, malformedError()
	}
	return root, nil
}

func malformedError() *source.Error {
	return source.Errorf(source.Malformed, "Unrecognised response from DeepSeek balance endpoint")
}
