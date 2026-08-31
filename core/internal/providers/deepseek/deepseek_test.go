package deepseek

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"quotamon/internal/fixtures"
	"quotamon/internal/snapshot"
	"quotamon/internal/source"
)

func TestLiveSourceReadsTheFixtureAsAnAvailableCNYBalance(t *testing.T) {
	fixture, err := fixtures.Load("deepseek-balance.json")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != "/user/balance" {
			t.Errorf("request = %s %s, want GET /user/balance", request.Method, request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("Authorization header = %q, want Bearer tok", request.Header.Get("Authorization"))
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write(fixture)
	}))
	defer server.Close()

	before := time.Now()
	provider, err := (LiveSource{BaseURL: server.URL, Client: server.Client(), Key: func() string { return "tok" }}).Fetch(context.Background())
	after := time.Now()
	if err != nil {
		t.Fatal(err)
	}
	if provider.ID != ProviderID || provider.DisplayName != DisplayName || provider.Plan != nil || provider.Origin != snapshot.OriginLive || provider.Status != snapshot.OK() {
		t.Fatalf("Fetch() metadata = %#v", provider)
	}
	if len(provider.Windows) != 0 {
		t.Fatalf("Fetch().Windows = %#v, want none for a balance-only provider", provider.Windows)
	}
	if provider.ObservedAt.Before(before) || provider.ObservedAt.After(after) {
		t.Fatalf("Fetch().ObservedAt = %s, want fetch time between %s and %s", provider.ObservedAt.Time, before, after)
	}
	assertCredits(t, provider.Credits, true, "110.00 CNY")
}

func TestLiveSourceRejectsMissingEmptyOrMalformedBalances(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "missing balance infos", body: `{"is_available":true}`},
		{name: "empty balance infos", body: `{"is_available":true,"balance_infos":[]}`},
		{name: "missing total balance", body: `{"is_available":true,"balance_infos":[{"currency":"CNY"}]}`},
		{name: "non string total balance", body: `{"is_available":true,"balance_infos":[{"currency":"CNY","total_balance":110.0}]}`},
		{name: "unparsable total balance", body: `{"is_available":true,"balance_infos":[{"currency":"CNY","total_balance":"credit"}]}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := fetchBody(t, test.body)
			var sourceError *source.Error
			if !errors.As(err, &sourceError) || sourceError.Kind != source.Malformed {
				t.Fatalf("Fetch() error = %v, want Malformed", err)
			}
		})
	}
}

func TestLiveSourcePrefersUSDAndFormatsOtherCurrenciesAfterTheAmount(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		wantBalance string
	}{
		{
			name:        "USD is preferred when CNY appears first",
			body:        `{"is_available":true,"balance_infos":[{"currency":"CNY","total_balance":"110.00"},{"currency":"USD","total_balance":"5.50"}]}`,
			wantBalance: "$5.50",
		},
		{
			name:        "CNY only uses the currency suffix",
			body:        `{"is_available":true,"balance_infos":[{"currency":"CNY","total_balance":"110.00"}]}`,
			wantBalance: "110.00 CNY",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			provider, err := fetchBody(t, test.body)
			if err != nil {
				t.Fatal(err)
			}
			assertCredits(t, provider.Credits, true, test.wantBalance)
		})
	}
}

func TestLiveSourceTreatsUnavailableZeroBalanceAsANormalState(t *testing.T) {
	provider, err := fetchBody(t, `{"is_available":false,"balance_infos":[{"currency":"USD","total_balance":"0.00"}]}`)
	if err != nil {
		t.Fatal(err)
	}
	assertCredits(t, provider.Credits, false, "$0.00")
}

func TestLiveSourceMapsAuthenticationFailuresToTheDeepSeekSetupMessage(t *testing.T) {
	tests := []struct {
		name   string
		status int
	}{
		{name: "401 rejects the key", status: http.StatusUnauthorized},
		{name: "403 rejects the key", status: http.StatusForbidden},
	}
	wantMessage := "DeepSeek API key rejected — set DEEPSEEK_KEY or run: quotamon config set deepseek --api-key-stdin"
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := fetchStatus(t, test.status)
			var sourceError *source.Error
			if !errors.As(err, &sourceError) || sourceError.Kind != source.Unauthorized || err.Error() != wantMessage {
				t.Fatalf("Fetch() error = %v, want Unauthorized %q", err, wantMessage)
			}
		})
	}
}

func TestLiveSourceMapsRateLimitingToTransport(t *testing.T) {
	err := fetchStatus(t, http.StatusTooManyRequests)
	var sourceError *source.Error
	if !errors.As(err, &sourceError) || sourceError.Kind != source.Transport || !strings.Contains(err.Error(), "rate limiting") {
		t.Fatalf("Fetch() error = %v, want rate-limited Transport", err)
	}
}

func fetchBody(t *testing.T, body string) (snapshot.Provider, error) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(body))
	}))
	defer server.Close()
	return (LiveSource{BaseURL: server.URL, Client: server.Client(), Key: func() string { return "tok" }}).Fetch(context.Background())
}

func fetchStatus(t *testing.T, status int) error {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(status)
	}))
	defer server.Close()
	_, err := (LiveSource{BaseURL: server.URL, Client: server.Client(), Key: func() string { return "tok" }}).Fetch(context.Background())
	return err
}

func assertCredits(t *testing.T, credits *snapshot.Credits, hasCredits bool, balance string) {
	t.Helper()
	if credits == nil || credits.Balance == nil {
		t.Fatalf("Credits = %#v, want a balance", credits)
	}
	if credits.HasCredits != hasCredits || credits.Unlimited || !credits.Enabled || *credits.Balance != balance || credits.Spend != nil {
		t.Fatalf("Credits = %#v, want HasCredits %v Balance %q and no spend", credits, hasCredits, balance)
	}
}
