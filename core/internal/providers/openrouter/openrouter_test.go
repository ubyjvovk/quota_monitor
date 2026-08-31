package openrouter

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

func TestLiveSourceReadsTheFixtureAsRemainingCreditsAndLifetimeSpend(t *testing.T) {
	fixture, err := fixtures.Load("openrouter-credits.json")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != "/api/v1/credits" {
			t.Errorf("request = %s %s, want GET /api/v1/credits", request.Method, request.URL.Path)
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
		t.Fatalf("Fetch().Windows = %#v, want none for a credits-only provider", provider.Windows)
	}
	if provider.ObservedAt.Before(before) || provider.ObservedAt.After(after) {
		t.Fatalf("Fetch().ObservedAt = %s, want fetch time between %s and %s", provider.ObservedAt.Time, before, after)
	}
	assertCredits(t, provider.Credits, true, "$10.63", "$14.37 all time")
}

func TestLiveSourceRejectsMissingOrNonNumericTotals(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "missing total credits", body: `{"data":{"total_usage":14.37}}`},
		{name: "non numeric total credits", body: `{"data":{"total_credits":"25.0","total_usage":14.37}}`},
		{name: "missing total usage", body: `{"data":{"total_credits":25.0}}`},
		{name: "non numeric total usage", body: `{"data":{"total_credits":25.0,"total_usage":"14.37"}}`},
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

func TestLiveSourceFloorsNegativeRemainingCreditAtZero(t *testing.T) {
	provider, err := fetchBody(t, `{"data":{"total_credits":10.0,"total_usage":12.0}}`)
	if err != nil {
		t.Fatal(err)
	}
	assertCredits(t, provider.Credits, false, "$0.00", "$12.00 all time")
}

func TestLiveSourceMapsAuthenticationFailuresToTheOpenRouterSetupMessage(t *testing.T) {
	tests := []struct {
		name   string
		status int
	}{
		{name: "401 rejects the key", status: http.StatusUnauthorized},
		{name: "403 rejects the key", status: http.StatusForbidden},
	}
	wantMessage := "OpenRouter API key rejected — set OPENROUTER_KEY or run: quotamon config set openrouter --api-key-stdin"
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

func assertCredits(t *testing.T, credits *snapshot.Credits, hasCredits bool, balance, spend string) {
	t.Helper()
	if credits == nil || credits.Balance == nil || credits.Spend == nil {
		t.Fatalf("Credits = %#v, want balance and spend", credits)
	}
	if credits.HasCredits != hasCredits || credits.Unlimited || !credits.Enabled || *credits.Balance != balance || *credits.Spend != spend {
		t.Fatalf("Credits = %#v, want HasCredits %v Balance %q Spend %q", credits, hasCredits, balance, spend)
	}
}
