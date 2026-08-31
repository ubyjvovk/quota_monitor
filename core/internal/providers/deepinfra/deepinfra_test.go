package deepinfra

import (
	"bytes"
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
	// Balance unknown (checklist unavailable) falls back to the spend-only
	// reading exactly as before this ticket.
	provider := Snapshot(-1, false, spent, periodEnd, observedAt, Balance{})

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
	provider := Snapshot(20, true, spent, periodEnd, observedAt, Balance{})

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
	var configRequests, usageRequests, checklistRequests atomic.Int32
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
		if request.URL.Path == "/payment/checklist" {
			checklistRequests.Add(1)
			if request.URL.RawQuery != "" {
				t.Errorf("checklist request query = %q, want none", request.URL.RawQuery)
			}
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"stripe_balance":-18.0,"recent":0.0,"suspended":false,"overdue_invoices":0.0}`))
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
	if configRequests.Load() != 1 || usageRequests.Load() != 1 || checklistRequests.Load() != 1 {
		t.Fatalf("request counts = config %d usage %d checklist %d, want one each", configRequests.Load(), usageRequests.Load(), checklistRequests.Load())
	}
	if len(provider.Windows) != 1 || provider.Windows[0].UsedPercent != 38.75 {
		t.Fatalf("Fetch() windows = %#v, want one monthly window at 38.75%%", provider.Windows)
	}
	assertPrepaidCredits(t, provider.Credits, true, "$18.00", true, "$7.75 this month")
}

func TestLiveSourceFetchesAllThreeEndpointsConcurrently(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		time.Sleep(300 * time.Millisecond)
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/payment/config":
			_, _ = writer.Write([]byte(`{"limit":20.0}`))
		case "/payment/usage":
			_, _ = writer.Write([]byte(`{"months":[{"total_cost":775}]}`))
		case "/payment/checklist":
			_, _ = writer.Write([]byte(`{"stripe_balance":-18.0}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	started := time.Now()
	_, err := (LiveSource{
		BaseURL: server.URL,
		Client:  server.Client(),
		Key:     func() string { return "tok" },
	}).Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed >= 500*time.Millisecond {
		t.Fatalf("Fetch() took %s, want less than 500ms for parallel calls", elapsed)
	}
}

func TestLiveSourceRejectsUsageMonthWithoutValidTotalCost(t *testing.T) {
	tests := []struct {
		name  string
		usage string
	}{
		{name: "missing total cost", usage: `{"months":[{}]}`},
		{name: "malformed total cost", usage: `{"months":[{"total_cost":null}]}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				writer.Header().Set("Content-Type", "application/json")
				switch request.URL.Path {
				case "/payment/config":
					_, _ = writer.Write([]byte(`{"limit":-1.0}`))
				case "/payment/usage":
					_, _ = writer.Write([]byte(test.usage))
				case "/payment/checklist":
					_, _ = writer.Write([]byte(`{}`))
				default:
					http.NotFound(writer, request)
				}
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

func TestLiveSourceReportsSpendWithoutBalanceWhenChecklistMoneyFieldsAreMissing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/payment/config":
			_, _ = writer.Write([]byte(`{"limit":-1.0}`))
		case "/payment/usage":
			_, _ = writer.Write([]byte(`{"months":[{"total_cost":775}]}`))
		case "/payment/checklist":
			_, _ = writer.Write([]byte(`{}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	provider, err := (LiveSource{BaseURL: server.URL, Client: server.Client(), Key: func() string { return "tok" }}).Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if provider.Credits == nil || provider.Credits.Spend == nil || *provider.Credits.Spend != "$7.75 this month" {
		t.Fatalf("Fetch().Credits.Spend = %v, want $7.75 this month", provider.Credits)
	}
	if provider.Credits.Balance != nil {
		t.Fatalf("Fetch().Credits.Balance = %q, want omitted for unknown checklist money", *provider.Credits.Balance)
	}
	if provider.Status != snapshot.OK() {
		t.Fatalf("Fetch().Status = %#v, want ok", provider.Status)
	}
}

func TestLiveSourceKeepsSpendWhenConfigFails(t *testing.T) {
	// Both the ceiling and the balance are unknown here, so only usage remains;
	// the spending-limit message is the one shown because config is the final
	// override, matching the pre-balance behaviour.
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/payment/config", "/payment/checklist":
			http.Error(writer, "unavailable", http.StatusInternalServerError)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"months":[{"total_cost":775}]}`))
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
	assertSpendCredits(t, provider.Credits, true, "$7.75 this month")
	if len(provider.Windows) != 0 {
		t.Fatalf("Fetch().Windows = %#v, want none without a known limit", provider.Windows)
	}
	wantMessage := "Spending limit unknown — DeepInfra /payment/config: " + source.ForHTTP(http.StatusInternalServerError, DisplayName).Error()
	if provider.Status != snapshot.NeedsSetup(wantMessage) {
		t.Fatalf("Fetch().Status = %#v, want NeedsSetup(%q)", provider.Status, wantMessage)
	}
}

func TestLiveSourceMapsPerCallDeadlineToSlowTransportError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/payment/usage" {
			time.Sleep(300 * time.Millisecond)
			_, _ = writer.Write([]byte(`{"months":[{"total_cost":775}]}`))
			return
		}
		_, _ = writer.Write([]byte(`{"limit":20.0}`))
	}))
	defer server.Close()

	_, err := (LiveSource{
		BaseURL:     server.URL,
		Client:      server.Client(),
		CallTimeout: 100 * time.Millisecond,
		Key:         func() string { return "tok" },
	}).Fetch(context.Background())
	var sourceError *source.Error
	if !errors.As(err, &sourceError) || sourceError.Kind != source.Transport {
		t.Fatalf("Fetch() error = %v, want Transport", err)
	}
	if !strings.Contains(err.Error(), "slow to answer (>0.1 s)") {
		t.Fatalf("Fetch() message = %q, want slow-to-answer explanation with actual cap", err)
	}
}

func TestLiveSourceDefaultCallTimeoutIsTwelveSeconds(t *testing.T) {
	if defaultCallTimeout != 12*time.Second {
		t.Fatalf("defaultCallTimeout = %s, want 12s", defaultCallTimeout)
	}
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	cancel()
	_, err := (LiveSource{}).fetchJSON(ctx, "https://example.invalid", http.DefaultClient, "tok", "/payment/usage", defaultCallTimeout)
	if err == nil || !strings.Contains(err.Error(), "slow to answer (>12 s)") {
		t.Fatalf("fetchJSON() error = %q, want slow-to-answer explanation with >12 s cap", err)
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

func TestStripeBalanceMappings(t *testing.T) {
	tests := []struct {
		name        string
		balance     Balance
		wantCredits bool
		wantBalance string
	}{
		{
			name:        "spendable balance is account funds minus recent usage",
			balance:     Balance{Known: true, Stripe: -18.0, StripeKnown: true, Recent: 7.97, RecentKnown: true},
			wantCredits: true, wantBalance: "$10.03",
		},
		{
			name:        "owed balance sums account debt and recent usage",
			balance:     Balance{Known: true, Stripe: 5.0, StripeKnown: true, Recent: 2.0, RecentKnown: true},
			wantCredits: false, wantBalance: "$7.00 owed",
		},
		{
			name:        "remaining funds down to cents still report spendable headroom",
			balance:     Balance{Known: true, Stripe: -8.0, StripeKnown: true, Recent: 7.97, RecentKnown: true},
			wantCredits: true, wantBalance: "$0.03",
		},
		{
			name:        "equal prepaid funds and recent usage report exactly zero",
			balance:     Balance{Known: true, Stripe: -8.0, StripeKnown: true, Recent: 8.0, RecentKnown: true},
			wantCredits: false, wantBalance: "$0.00",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			provider := Snapshot(-1, false, 7.75, nil, observedAt, test.balance)
			assertPrepaidCredits(t, provider.Credits, test.wantCredits, test.wantBalance, true, "$7.75 this month")
			if len(provider.Windows) != 0 || provider.Status != snapshot.OK() {
				t.Fatalf("Snapshot() = windows %#v status %#v, want none and ok", provider.Windows, provider.Status)
			}
		})
	}
}

func TestSuspendedAndOverdueInvoicesSetActionableStatus(t *testing.T) {
	suspended := Snapshot(-1, false, 7.75, nil, observedAt, Balance{Known: true, Stripe: -18.0, StripeKnown: true, Recent: 7.97, RecentKnown: true, Suspended: true, SuspendReason: "unpaid balance"})
	assertPrepaidCredits(t, suspended.Credits, true, "$10.03", false, "$7.75 this month")
	if suspended.Status.State != "failed" || !strings.Contains(suspended.Status.Message, "suspended") || !strings.Contains(suspended.Status.Message, "unpaid balance") {
		t.Fatalf("Snapshot().Status = %#v, want failed with suspend reason", suspended.Status)
	}

	overdue := Snapshot(-1, false, 7.75, nil, observedAt, Balance{Known: true, OverdueInvoices: 2})
	wantStatus := snapshot.NeedsSetup("DeepInfra has 2 overdue invoice(s)")
	if overdue.Status != wantStatus {
		t.Fatalf("Snapshot().Status = %#v, want %#v", overdue.Status, wantStatus)
	}
}

func TestChecklistKeepsParsedMoneyComponentsUnknownSeparately(t *testing.T) {
	root, err := jsonx.Parse([]byte(`{"stripe_balance":-18.0,"recent":"drift"}`))
	if err != nil {
		t.Fatal(err)
	}

	balance := balanceFromChecklist(root)
	if !balance.StripeKnown || balance.Stripe != -18.0 {
		t.Fatalf("balance stripe = %#v, want parsed -18.0 component", balance)
	}
	if balance.RecentKnown {
		t.Fatalf("balance recent = %#v, want unknown malformed component", balance)
	}
	provider := Snapshot(-1, false, 7.75, nil, observedAt, balance)
	if provider.Credits == nil || provider.Credits.Balance != nil {
		t.Fatalf("Snapshot().Credits.Balance = %v, want omitted for incomplete money fields", provider.Credits)
	}
}

func TestLiveSourceCombinesPrepaidBalanceAndSpend(t *testing.T) {
	usage, err := fixtures.Load("deepinfra-usage.json")
	if err != nil {
		t.Fatal(err)
	}
	checklist, err := fixtures.Load("deepinfra-checklist.json")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/payment/config":
			_, _ = writer.Write([]byte(`{"limit":-1.0}`))
		case "/payment/usage":
			_, _ = writer.Write(usage)
		case "/payment/checklist":
			_, _ = writer.Write(checklist)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	provider, err := (LiveSource{BaseURL: server.URL, Client: server.Client(), Key: func() string { return "tok" }}).Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	assertPrepaidCredits(t, provider.Credits, true, "$10.03", true, "$7.75 this month")
	if len(provider.Windows) != 0 {
		t.Fatalf("Fetch().Windows = %#v, want none with a negative limit", provider.Windows)
	}
	if provider.Status != snapshot.OK() {
		t.Fatalf("Fetch().Status = %#v, want ok", provider.Status)
	}
}

func TestLiveSourceKeepsSpendOnlyWhenChecklistFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/payment/config":
			_, _ = writer.Write([]byte(`{"limit":-1.0}`))
		case "/payment/usage":
			_, _ = writer.Write([]byte(`{"months":[{"total_cost":775}]}`))
		case "/payment/checklist":
			http.Error(writer, "boom", http.StatusInternalServerError)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	provider, err := (LiveSource{BaseURL: server.URL, Client: server.Client(), Key: func() string { return "tok" }}).Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	assertSpendCredits(t, provider.Credits, true, "$7.75 this month")
	wantMessage := "Balance unavailable — DeepInfra /payment/checklist: " + source.ForHTTP(http.StatusInternalServerError, DisplayName).Error()
	if provider.Status != snapshot.NeedsSetup(wantMessage) {
		t.Fatalf("Fetch().Status = %#v, want NeedsSetup(%q)", provider.Status, wantMessage)
	}
}

func TestChecklistPiiNeverAppearsInEncodedSnapshot(t *testing.T) {
	root, err := jsonx.Parse([]byte(`{"stripe_balance":-18.0,"suspended":false,"overdue_invoices":0.0,"billing_address_info":{"line1":"1 Main St","postal_code":"90210"}}`))
	if err != nil {
		t.Fatal(err)
	}
	provider := Snapshot(-1, false, 7.75, nil, observedAt, balanceFromChecklist(root))
	encoded, err := (snapshot.Snapshot{
		Providers:   []snapshot.Provider{provider},
		GeneratedAt: snapshot.Time{Time: observedAt},
	}).Encode()
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"line1", "postal_code", "Main"} {
		if bytes.Contains(encoded, []byte(forbidden)) {
			t.Fatalf("encoded snapshot contains %q from billing_address_info:\n%s", forbidden, encoded)
		}
	}
}

// assertPrepaidCredits checks a prepaid-balance credits contract: a spendable
// balance distinct from the month-to-date spend.
func assertPrepaidCredits(t *testing.T, credits *snapshot.Credits, hasCredits bool, balance string, enabled bool, spend string) {
	t.Helper()
	if credits == nil {
		t.Fatal("Snapshot().Credits is nil")
	}
	if credits.HasCredits != hasCredits || credits.Unlimited || credits.Enabled != enabled {
		t.Fatalf("Snapshot().Credits = %#v, want HasCredits %v Unlimited false Enabled %v", credits, hasCredits, enabled)
	}
	if credits.Balance == nil || *credits.Balance != balance {
		t.Fatalf("Snapshot().Credits.Balance = %v, want %q", credits.Balance, balance)
	}
	if credits.Spend == nil || *credits.Spend != spend {
		t.Fatalf("Snapshot().Credits.Spend = %v, want %q", credits.Spend, spend)
	}
}

// assertSpendCredits checks the spend-only credits contract: never a spendable
// balance, so HasCredits is always false, with spend reported as "X this month".
func assertSpendCredits(t *testing.T, credits *snapshot.Credits, unlimited bool, spend string) {
	t.Helper()
	if credits == nil {
		t.Fatal("Snapshot().Credits is nil")
	}
	if credits.HasCredits || credits.Unlimited != unlimited || !credits.Enabled {
		t.Fatalf("Snapshot().Credits = %#v, want HasCredits false Unlimited %v Enabled true", credits, unlimited)
	}
	if credits.Balance != nil {
		t.Fatalf("Snapshot().Credits.Balance = %q, want omitted", *credits.Balance)
	}
	if credits.Spend == nil || *credits.Spend != spend {
		t.Fatalf("Snapshot().Credits.Spend = %v, want %q", credits.Spend, spend)
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
