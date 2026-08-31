package runinfra

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"quotamon/internal/fixtures"
	"quotamon/internal/snapshot"
	"quotamon/internal/source"
)

var observedAt = time.Date(2026, 8, 31, 20, 0, 0, 0, time.UTC)

func TestSnapshotMapsAvailableCentsAndSpendAndHardCapFromTheFixture(t *testing.T) {
	provider := Snapshot("pro", 2500, 785, 15.7, true, observedAt)

	if provider.ID != ProviderID || provider.DisplayName != DisplayName || provider.Origin != snapshot.OriginLive {
		t.Fatalf("Snapshot() metadata = %#v", provider)
	}
	if provider.Plan == nil || *provider.Plan != "pro" {
		t.Fatalf("Snapshot().Plan = %v, want pro", provider.Plan)
	}
	if provider.Credits == nil || provider.Credits.Balance == nil || *provider.Credits.Balance != "$25.00" {
		t.Fatalf("Snapshot().Credits.Balance = %v, want $25.00 from available_cents", provider.Credits)
	}
	if provider.Credits.Spend == nil || *provider.Credits.Spend != "$7.85 this month" {
		t.Fatalf("Snapshot().Credits.Spend = %v, want $7.85 this month", provider.Credits)
	}
	if !provider.Credits.HasCredits || provider.Credits.Unlimited || !provider.Credits.Enabled {
		t.Fatalf("Snapshot().Credits = %#v, want HasCredits true Unlimited false Enabled true", provider.Credits)
	}
	if len(provider.Windows) != 1 {
		t.Fatalf("Snapshot().Windows = %#v, want one hard-cap window", provider.Windows)
	}
	window := provider.Windows[0]
	if window.ID != "monthly_cap" || window.Label != "Cap" || window.Kind != snapshot.KindMonthly || window.UsedPercent != 15.7 {
		t.Fatalf("Snapshot().Windows[0] = %#v, want Cap monthly at 15.7%%", window)
	}
	if window.ResetsAt != nil || window.WindowMinutes != nil {
		t.Fatalf("Snapshot().Windows[0] = %#v, want no reset and no window minutes (the API gives neither)", window)
	}
	if provider.Status != snapshot.OK() {
		t.Fatalf("Snapshot().Status = %#v, want ok", provider.Status)
	}
}

func TestSnapshotOmitsPlanWhenTheTierIsUnset(t *testing.T) {
	provider := Snapshot("", 2500, 785, 0, false, observedAt)
	if provider.Plan != nil {
		t.Fatalf("Snapshot().Plan = %v, want nil when plan_tier is empty", provider.Plan)
	}
}

func TestSnapshotShowsZeroCreditsWhenAvailableIsZero(t *testing.T) {
	provider := Snapshot("pro", 0, 785, 0, false, observedAt)
	if provider.Credits == nil || provider.Credits.Balance == nil || *provider.Credits.Balance != "$0.00" || provider.Credits.HasCredits {
		t.Fatalf("Snapshot().Credits = %#v, want $0.00 balance with HasCredits false", provider.Credits)
	}
}

func TestLiveSourceReadsTheFixtureAndMapsTheRealShape(t *testing.T) {
	fixture, err := fixtures.Load("runinfra-credits.json")
	if err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		if request.Method != http.MethodGet {
			t.Errorf("request method = %s, want GET", request.Method)
		}
		if request.URL.Path != "/v1/credits" {
			t.Errorf("request path = %q, want /v1/credits", request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("Authorization header = %q, want Bearer tok", request.Header.Get("Authorization"))
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write(fixture)
	}))
	defer server.Close()

	provider, err := (LiveSource{
		BaseURL: server.URL,
		Client:  server.Client(),
		Key:     func() string { return "tok" },
	}).Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 1 {
		t.Fatalf("Fetch() made %d requests, want one", requests.Load())
	}
	if provider.Credits == nil || provider.Credits.Balance == nil || *provider.Credits.Balance != "$25.00" {
		t.Fatalf("Fetch().Credits.Balance = %v, want $25.00 from the fixture", provider.Credits)
	}
	if len(provider.Windows) != 1 || provider.Windows[0].UsedPercent != 15.7 {
		t.Fatalf("Fetch().Windows = %#v, want one cap window at 15.7%%", provider.Windows)
	}
	if provider.Plan == nil || *provider.Plan != "pro" {
		t.Fatalf("Fetch().Plan = %v, want pro", provider.Plan)
	}
	// as_of 2026-08-31T19:48:55.384Z; the fractional seconds parse and the
	// observed reading keeps them because Snapshot stores the raw timestamp.
	wantObserved := time.Date(2026, 8, 31, 19, 48, 55, 384_000_000, time.UTC)
	if !provider.ObservedAt.Equal(wantObserved) {
		t.Fatalf("Fetch().ObservedAt = %s, want %s", provider.ObservedAt.Time, wantObserved)
	}
}

func TestLiveSourceWithAbsentOrSoftCapYieldsNoWindow(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "null cap leaves the reading windowless", body: `{"available_cents":2500,"period":{"spent_cents":785},"spend_cap":null}`},
		{name: "soft cap is advisory and must not read as quota pressure", body: `{"available_cents":2500,"period":{"spent_cents":785},"spend_cap":{"limit_cents":5000,"hard":false,"used_cents":785,"gates_inference":false}}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.Header().Set("Content-Type", "application/json")
				_, _ = writer.Write([]byte(test.body))
			}))
			defer server.Close()

			provider, err := (LiveSource{BaseURL: server.URL, Client: server.Client(), Key: func() string { return "tok" }}).Fetch(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if len(provider.Windows) != 0 {
				t.Fatalf("Fetch().Windows = %#v, want none without a hard cap", provider.Windows)
			}
			if provider.Credits == nil || provider.Credits.Balance == nil || *provider.Credits.Balance != "$25.00" {
				t.Fatalf("Fetch().Credits = %#v, want the available balance to survive", provider.Credits)
			}
		})
	}
}

func TestLiveSourceRejectsMissingOrMalformedMoney(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "missing available cents", body: `{"period":{"spent_cents":785}}`},
		{name: "malformed available cents", body: `{"available_cents":null,"period":{"spent_cents":785}}`},
		{name: "missing period spend", body: `{"available_cents":2500,"period":{}}`},
		{name: "malformed period spend", body: `{"available_cents":2500,"period":{"spent_cents":"drift"}}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.Header().Set("Content-Type", "application/json")
				_, _ = writer.Write([]byte(test.body))
			}))
			defer server.Close()

			_, err := (LiveSource{BaseURL: server.URL, Client: server.Client(), Key: func() string { return "tok" }}).Fetch(context.Background())
			var sourceError *source.Error
			if !errors.As(err, &sourceError) || sourceError.Kind != source.Malformed {
				t.Fatalf("Fetch() error = %v, want Malformed", err)
			}
		})
	}
}

func TestLiveSourceWithEmptyKeyMakesNoRequest(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
	}))
	defer server.Close()

	_, err := (LiveSource{BaseURL: server.URL, Client: server.Client(), Key: func() string { return "" }}).Fetch(context.Background())
	if requests.Load() != 0 {
		t.Fatalf("Fetch() made %d requests with an empty key, want zero", requests.Load())
	}
	var sourceError *source.Error
	if !errors.As(err, &sourceError) || sourceError.Kind != source.NotConfigured {
		t.Fatalf("Fetch() error = %v, want NotConfigured", err)
	}
	if !strings.Contains(err.Error(), "RUNINFRA_TOKEN") {
		t.Fatalf("Fetch() message = %q, want RUNINFRA_TOKEN action", err)
	}
}

func TestLiveSourceMapsAuthenticationFailuresToNeedsSetupRunInfraMessage(t *testing.T) {
	tests := []struct {
		name   string
		status int
	}{
		{name: "401 rejects the key", status: http.StatusUnauthorized},
		{name: "403 rejects the key", status: http.StatusForbidden},
	}
	wantMessage := "RunInfra API key rejected — set RUNINFRA_TOKEN or run: quotamon config set runinfra --api-key-stdin"
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(test.status)
			}))
			defer server.Close()
			_, err := (LiveSource{BaseURL: server.URL, Client: server.Client(), Key: func() string { return "tok" }}).Fetch(context.Background())
			var sourceError *source.Error
			if !errors.As(err, &sourceError) || sourceError.Kind != source.Unauthorized {
				t.Fatalf("Fetch() error = %v, want Unauthorized", err)
			}
			if err.Error() != wantMessage {
				t.Fatalf("Fetch() message = %q, want the setup action %q", err.Error(), wantMessage)
			}
		})
	}
}

func TestLiveSourceMapsRateLimitAndOtherFailuresToTransport(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		wantDetail string
	}{
		{name: "rate limited", status: http.StatusTooManyRequests, wantDetail: "rate limiting usage checks"},
		{name: "server unavailable", status: http.StatusInternalServerError, wantDetail: "is unavailable"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(test.status)
			}))
			defer server.Close()
			_, err := (LiveSource{BaseURL: server.URL, Client: server.Client(), Key: func() string { return "tok" }}).Fetch(context.Background())
			var sourceError *source.Error
			if !errors.As(err, &sourceError) || sourceError.Kind != source.Transport {
				t.Fatalf("Fetch() error = %v, want Transport", err)
			}
			if !strings.Contains(err.Error(), test.wantDetail) {
				t.Fatalf("Fetch() message = %q, want to mention %q", err.Error(), test.wantDetail)
			}
		})
	}
}

func TestLiveSourceDefaultCallTimeoutIsTwelveSeconds(t *testing.T) {
	if defaultCallTimeout != 12*time.Second {
		t.Fatalf("defaultCallTimeout = %s, want 12s", defaultCallTimeout)
	}
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	cancel()
	_, err := (LiveSource{}).fetchJSON(ctx, "https://example.invalid", http.DefaultClient, "tok", defaultCallTimeout)
	if err == nil || !strings.Contains(err.Error(), "slow to answer (>12 s)") {
		t.Fatalf("fetchJSON() error = %q, want slow-to-answer explanation with >12 s cap", err)
	}
}
