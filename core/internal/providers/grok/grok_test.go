package grok

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"quotamon/internal/fixtures"
	"quotamon/internal/jsonx"
	"quotamon/internal/snapshot"
	"quotamon/internal/source"
)

func TestBillingFixtureProducesOneSharedWeeklyWindow(t *testing.T) {
	root := fixtureRoot(t, "grok-billing-credits.json")
	observedAt := time.Date(2026, 8, 29, 20, 0, 0, 0, time.UTC)
	provider, ok := Snapshot(root, observedAt)
	if !ok {
		t.Fatal("Snapshot() found no window")
	}

	if provider.ID != ProviderID || provider.DisplayName != DisplayName || provider.Origin != snapshot.OriginLive || provider.Plan != nil {
		t.Fatalf("Snapshot() metadata = %#v", provider)
	}
	if provider.Credits != nil {
		t.Fatalf("Snapshot().Credits = %#v, want nil for zero prepaid balance", provider.Credits)
	}
	assertFixtureWindow(t, provider.Windows)
}

func TestCredentialParserSelectsOnlyTheFirstSortedXAIScope(t *testing.T) {
	tests := []struct {
		name      string
		blob      string
		wantToken string
	}{
		{
			name:      "xAI scope wins over unrelated credential-shaped data",
			blob:      `{"https://auth.x.ai::abc":{"key":"tok","expires_at":1788038896,"email":"x@y"},"other":{"key":"wrong"}}`,
			wantToken: "tok",
		},
		{
			name:      "sorted scope order is deterministic",
			blob:      `{"https://auth.x.ai::z":{"key":"later"},"https://auth.x.ai::a":{"key":"first"}}`,
			wantToken: "first",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			credentials, err := ParseCredentials([]byte(test.blob))
			if err != nil {
				t.Fatal(err)
			}
			if credentials.Token != test.wantToken {
				t.Fatalf("ParseCredentials().Token = %q, want %q", credentials.Token, test.wantToken)
			}
			if strings.Contains(test.blob, "expires_at") && (credentials.ExpiresAt == nil || credentials.ExpiresAt.Year() != 2026) {
				t.Fatalf("ParseCredentials().ExpiresAt = %v, want year 2026", credentials.ExpiresAt)
			}
		})
	}

	t.Run("missing xAI scope asks the user to log in", func(t *testing.T) {
		_, err := ParseCredentials([]byte(`{"other":{"key":"wrong"}}`))
		var sourceError *source.Error
		if !errors.As(err, &sourceError) || sourceError.Kind != source.NotConfigured {
			t.Fatalf("ParseCredentials() error = %v, want NotConfigured", err)
		}
		if !strings.Contains(sourceError.Message, "`grok login`") {
			t.Fatalf("ParseCredentials() message = %q, want grok login action", sourceError.Message)
		}
	})
}

func TestLiveSourceUsesGrokBillingEndpointAndClientMode(t *testing.T) {
	fixture, err := fixtures.Load("grok-billing-credits.json")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != "/v1/billing" || request.URL.Query().Get("format") != "credits" {
			t.Errorf("request = %s %s", request.Method, request.URL.String())
		}
		headers := map[string]string{
			"Authorization":      "Bearer tok",
			"x-grok-client-mode": "grok-build",
			"Accept":             "application/json",
			"User-Agent":         "quotamon/0.1",
		}
		for name, want := range headers {
			if got := request.Header.Get(name); got != want {
				t.Errorf("header %s = %q, want %q", name, got, want)
			}
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write(fixture)
	}))
	defer server.Close()

	provider, err := (LiveSource{
		BaseURL: server.URL,
		Client:  server.Client(),
		Credentials: func() ([]byte, error) {
			return []byte(`{"https://auth.x.ai::abc":{"key":"tok"}}`), nil
		},
	}).Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	assertFixtureWindow(t, provider.Windows)
}

func TestLiveSourceMapsAuthenticationFailuresToUnauthorized(t *testing.T) {
	tests := []struct {
		name   string
		status int
	}{
		{name: "401 asks the user to log in", status: http.StatusUnauthorized},
		{name: "403 asks the user to log in", status: http.StatusForbidden},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(test.status)
			}))
			defer server.Close()
			_, err := (LiveSource{
				BaseURL: server.URL,
				Client:  server.Client(),
				Credentials: func() ([]byte, error) {
					return []byte(`{"https://auth.x.ai::abc":{"key":"tok"}}`), nil
				},
			}).Fetch(context.Background())
			var sourceError *source.Error
			if !errors.As(err, &sourceError) || sourceError.Kind != source.Unauthorized {
				t.Fatalf("LiveSource.Fetch() error = %v, want Unauthorized", err)
			}
			if !strings.Contains(sourceError.Message, "`grok login`") {
				t.Fatalf("LiveSource.Fetch() message = %q, want grok login action", sourceError.Message)
			}
		})
	}
}

func TestSnapshotUsesFallbackDurationAndPositivePrepaidBalance(t *testing.T) {
	root, err := jsonx.Parse([]byte(`{
		"config":{
			"creditUsagePercent":12,
			"currentPeriod":{"type":"USAGE_PERIOD_TYPE_WEEKLY"},
			"prepaidBalance":{"val":4.5}
		}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	provider, ok := Snapshot(root, time.Now())
	if !ok {
		t.Fatal("Snapshot() found no window")
	}
	if provider.Windows[0].WindowMinutes == nil || *provider.Windows[0].WindowMinutes != 10080 {
		t.Fatalf("Snapshot().WindowMinutes = %v, want 10080", provider.Windows[0].WindowMinutes)
	}
	if provider.Credits == nil || !provider.Credits.HasCredits || !provider.Credits.Enabled || provider.Credits.Unlimited || provider.Credits.Balance == nil || *provider.Credits.Balance != "4.50" {
		t.Fatalf("Snapshot().Credits = %#v", provider.Credits)
	}
}

func assertFixtureWindow(t *testing.T, windows []snapshot.Window) {
	t.Helper()
	if len(windows) != 1 {
		t.Fatalf("windows count = %d, want 1: %#v", len(windows), windows)
	}
	window := windows[0]
	if window.ID != "credits" || window.Label != "Week" || window.Kind != snapshot.KindWeekly || window.UsedPercent != 63 {
		t.Fatalf("window = %#v", window)
	}
	wantReset := time.Date(2026, 9, 1, 11, 9, 21, 745222000, time.UTC)
	if window.ResetsAt == nil || !window.ResetsAt.Equal(wantReset) {
		t.Fatalf("window reset = %v, want %s", window.ResetsAt, wantReset)
	}
	if window.WindowMinutes == nil || *window.WindowMinutes != 10080 {
		t.Fatalf("window minutes = %v, want 10080", window.WindowMinutes)
	}
}

func fixtureRoot(t *testing.T, name string) any {
	t.Helper()
	data, err := fixtures.Load(name)
	if err != nil {
		t.Fatal(err)
	}
	root, err := jsonx.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	return root
}
