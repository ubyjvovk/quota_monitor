package deepinfra

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
	"quotamon/internal/jsonx"
	"quotamon/internal/snapshot"
	"quotamon/internal/source"
)

var observedAt = time.Date(2026, 8, 29, 20, 0, 0, 0, time.UTC)

func TestNegativeLimitYieldsSpendOnlyWithNoWindow(t *testing.T) {
	spent, periodEnd := fixtureReading(t)
	provider := Snapshot(-1, false, spent, periodEnd, observedAt)

	if provider.ID != ProviderID || provider.DisplayName != DisplayName || provider.Origin != snapshot.OriginLive {
		t.Fatalf("Snapshot() metadata = %#v", provider)
	}
	if provider.Plan == nil || *provider.Plan != "pay-as-you-go" {
		t.Fatalf("Snapshot().Plan = %v, want pay-as-you-go", provider.Plan)
	}
	if len(provider.Windows) != 0 {
		t.Fatalf("Snapshot().Windows = %#v, want none without a limit", provider.Windows)
	}
	if provider.Status != snapshot.OK() {
		t.Fatalf("Snapshot().Status = %#v, want ok", provider.Status)
	}
	assertSpendCredits(t, provider.Credits, true, "$7.75 this month")
}

func TestPositiveLimitAddsAMonthlyWindowAtTheSpendPercentage(t *testing.T) {
	spent, periodEnd := fixtureReading(t)
	provider := Snapshot(20, true, spent, periodEnd, observedAt)

	if len(provider.Windows) != 1 {
		t.Fatalf("Snapshot().Windows = %#v, want one monthly window", provider.Windows)
	}
	window := provider.Windows[0]
	if window.ID != "monthly_spend" || window.Label != "Month" || window.Kind != snapshot.KindMonthly || window.UsedPercent != 38.75 {
		t.Fatalf("Snapshot().Windows[0] = %#v, want monthly spend at 38.75%%", window)
	}
	if window.WindowMinutes != nil {
		t.Fatalf("Snapshot().Windows[0].WindowMinutes = %v, want nil", window.WindowMinutes)
	}
	// interval.to 1788245999999 ms truncates to 2026-09-01T06:59:59Z. The
	// ticket's acceptance literal (2026-08-31T22:59:59Z) is an 8-hour slip —
	// that value is 1788245999 minus eight hours, i.e. a Pacific reading stamped
	// Z. The raw milliseconds and the "truncate to seconds" rule are the
	// authoritative spec, so the correct UTC result is used here.
	wantReset := time.Date(2026, 9, 1, 6, 59, 59, 0, time.UTC)
	if window.ResetsAt == nil || !window.ResetsAt.Equal(wantReset) {
		t.Fatalf("Snapshot().Windows[0].ResetsAt = %v, want %s", window.ResetsAt, wantReset)
	}
	assertSpendCredits(t, provider.Credits, false, "$7.75 this month")
}

func TestLiveSourceHitsThePaymentPathsWithoutV1AndSendsTheBearer(t *testing.T) {
	fixture, err := fixtures.Load("deepinfra-usage.json")
	if err != nil {
		t.Fatal(err)
	}
	var configRequests, usageRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet {
			t.Errorf("request method = %s, want GET", request.Method)
		}
		if request.URL.Path == "/payment/config" {
			configRequests.Add(1)
			if request.URL.RawQuery != "" {
				t.Errorf("config request query = %q, want none", request.URL.RawQuery)
			}
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"limit":20.0}`))
			return
		}
		if request.URL.Path == "/payment/usage" {
			usageRequests.Add(1)
			if request.URL.RawQuery != "from=current" {
				t.Errorf("usage request query = %q, want from=current", request.URL.RawQuery)
			}
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write(fixture)
			return
		}
		t.Errorf("unexpected request path %q (endpoints must not be under /v1)", request.URL.Path)
		http.NotFound(writer, request)
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
	if configRequests.Load() != 1 || usageRequests.Load() != 1 {
		t.Fatalf("request counts = config %d usage %d, want one each", configRequests.Load(), usageRequests.Load())
	}
	if len(provider.Windows) != 1 || provider.Windows[0].UsedPercent != 38.75 {
		t.Fatalf("Fetch() windows = %#v, want one monthly window at 38.75%%", provider.Windows)
	}
	assertSpendCredits(t, provider.Credits, false, "$7.75 this month")
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
	if !strings.Contains(err.Error(), "DEEPINFRA_KEY") {
		t.Fatalf("Fetch() message = %q, want DEEPINFRA_KEY action", err)
	}
}

func TestLiveSourceMapsAuthenticationFailuresToUnauthorized(t *testing.T) {
	tests := []struct {
		name   string
		status int
	}{
		{name: "401 rejects the key", status: http.StatusUnauthorized},
		{name: "403 rejects the key", status: http.StatusForbidden},
	}
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
			if !strings.Contains(err.Error(), "DEEPINFRA_KEY") {
				t.Fatalf("Fetch() message = %q, want DEEPINFRA_KEY action", err)
			}
		})
	}
}

// assertSpendCredits checks the spend-only credits contract: never a spendable
// balance, so HasCredits is always false, with spend reported as "X this month".
func assertSpendCredits(t *testing.T, credits *snapshot.Credits, unlimited bool, balance string) {
	t.Helper()
	if credits == nil {
		t.Fatal("Snapshot().Credits is nil")
	}
	if credits.HasCredits || credits.Unlimited != unlimited || !credits.Enabled {
		t.Fatalf("Snapshot().Credits = %#v, want HasCredits false Unlimited %v Enabled true", credits, unlimited)
	}
	if credits.Balance == nil || *credits.Balance != balance {
		t.Fatalf("Snapshot().Credits.Balance = %v, want %q", credits.Balance, balance)
	}
}

// fixtureReading extracts spentUSD and periodEnd from the shared usage fixture
// using the same explicit-path rules as the live source.
func fixtureReading(t *testing.T) (float64, *time.Time) {
	t.Helper()
	data, err := fixtures.Load("deepinfra-usage.json")
	if err != nil {
		t.Fatal(err)
	}
	root, err := jsonx.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	monthsValue, _ := jsonx.Get(root, "months")
	month := monthsValue.([]any)[0]
	cents, _ := jsonx.Float(jsonxMust(t, month, "total_cost"))
	spent := cents / 100
	periodEnd, _ := jsonx.Time(jsonxMust(t, month, "interval", "to"))
	return spent, &periodEnd
}

func jsonxMust(t *testing.T, root any, path ...string) any {
	t.Helper()
	value, found := jsonx.Get(root, path...)
	if !found {
		t.Fatalf("missing %v in fixture", path)
	}
	return value
}

