package claude

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"quotamon/internal/fixtures"
	"quotamon/internal/jsonx"
	"quotamon/internal/snapshot"
	"quotamon/internal/source"
)

func TestLiveFixtureKeepsCanonicalLimitsAndDisabledSpend(t *testing.T) {
	root := fixtureRoot(t, "claude-usage-live.json")
	observedAt := time.Date(2026, 8, 29, 19, 0, 0, 0, time.UTC)
	provider, ok := Snapshot(root, observedAt, snapshot.OriginLive, "max")
	if !ok {
		t.Fatal("Snapshot() found no windows")
	}

	assertCanonicalWindows(t, provider.Windows)
	for _, window := range provider.Windows {
		if strings.Contains(window.ID, "nimbus_quill") {
			t.Fatalf("Snapshot() included codenamed window %q", window.ID)
		}
	}
	if provider.Credits == nil {
		t.Fatal("Snapshot().Credits is nil")
	}
	if provider.Credits.HasCredits || provider.Credits.Unlimited || provider.Credits.Enabled {
		t.Fatalf("Snapshot().Credits = %#v, want disabled finite credits", provider.Credits)
	}
	if provider.Credits.Balance == nil || *provider.Credits.Balance != "20.00" {
		t.Fatalf("Snapshot().Credits.Balance = %v, want 20.00", provider.Credits.Balance)
	}
}

func TestLocalSourceReadsTheLegacyMirror(t *testing.T) {
	fixture, err := fixtures.Load("claude-mirror.json")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "claude-usage.json")
	if err := os.WriteFile(path, fixture, 0o600); err != nil {
		t.Fatal(err)
	}

	provider, err := (LocalSource{MirrorPath: path}).Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if provider.Origin != snapshot.OriginLocal || provider.Plan == nil || *provider.Plan != "max" {
		t.Fatalf("LocalSource.Fetch() metadata = origin %q plan %v", provider.Origin, provider.Plan)
	}
	wantObservedAt := time.Date(2026, 7, 31, 19, 40, 0, 0, time.UTC)
	if !provider.ObservedAt.Equal(wantObservedAt) {
		t.Fatalf("LocalSource.Fetch().ObservedAt = %s, want %s", provider.ObservedAt.Time, wantObservedAt)
	}
	if len(provider.Windows) != 2 {
		t.Fatalf("LocalSource.Fetch() returned %d windows, want 2", len(provider.Windows))
	}
	tests := []struct {
		name        string
		index       int
		id          string
		usedPercent float64
		resetsAt    time.Time
	}{
		{name: "five-hour usage is preserved", index: 0, id: "five_hour", usedPercent: 63.4, resetsAt: time.Unix(1785790000, 0).UTC()},
		{name: "seven-day usage is preserved", index: 1, id: "seven_day", usedPercent: 21.9, resetsAt: time.Unix(1786300000, 0).UTC()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			window := provider.Windows[test.index]
			if window.ID != test.id || window.UsedPercent != test.usedPercent {
				t.Fatalf("window = %#v, want id %q usage %v", window, test.id, test.usedPercent)
			}
			if window.ResetsAt == nil || !window.ResetsAt.Equal(test.resetsAt) {
				t.Fatalf("window reset = %v, want %s", window.ResetsAt, test.resetsAt)
			}
		})
	}
}

func TestCredentialParserAddressesOnlyClaudeOAuth(t *testing.T) {
	tests := []struct {
		name     string
		blob     string
		wantPlan string
	}{
		{
			name: "nested Claude token wins over an MCP token",
			blob: `{
				"claudeAiOauth":{"accessToken":"right","expiresAt":1788038896000,"subscriptionType":"max"},
				"mcpOAuth":{"x":{"accessToken":"wrong"}}
			}`,
			wantPlan: "max",
		},
		{
			name:     "legacy top-level Claude fields remain supported",
			blob:     `{"accessToken":"right","expiresAt":1788038896000,"subscriptionType":"pro"}`,
			wantPlan: "pro",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			credentials, err := ParseCredentials([]byte(test.blob))
			if err != nil {
				t.Fatal(err)
			}
			if credentials.Token != "right" || credentials.Plan != test.wantPlan {
				t.Fatalf("ParseCredentials() = %#v", credentials)
			}
			if credentials.ExpiresAt == nil || credentials.ExpiresAt.Year() != 2026 {
				t.Fatalf("ParseCredentials().ExpiresAt = %v, want year 2026", credentials.ExpiresAt)
			}
		})
	}

	t.Run("an empty token is not configured", func(t *testing.T) {
		_, err := ParseCredentials([]byte(`{"claudeAiOauth":{"accessToken":""}}`))
		var sourceError *source.Error
		if !errors.As(err, &sourceError) || sourceError.Kind != source.NotConfigured {
			t.Fatalf("ParseCredentials() error = %v, want NotConfigured", err)
		}
	})
}

func TestLiveSourceUsesClaudeOAuthEndpoint(t *testing.T) {
	fixture, err := fixtures.Load("claude-usage-live.json")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != "/api/oauth/usage" {
			t.Errorf("request = %s %s", request.Method, request.URL.Path)
		}
		headers := map[string]string{
			"Authorization":     "Bearer right",
			"anthropic-beta":    "oauth-2025-04-20",
			"anthropic-version": "2023-06-01",
			"Accept":            "application/json",
			"User-Agent":        "quotamon/0.1",
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
			return []byte(`{"claudeAiOauth":{"accessToken":"right","subscriptionType":"max"}}`), nil
		},
	}).Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if provider.Origin != snapshot.OriginLive || provider.Plan == nil || *provider.Plan != "max" {
		t.Fatalf("LiveSource.Fetch() metadata = origin %q plan %v", provider.Origin, provider.Plan)
	}
	assertCanonicalWindows(t, provider.Windows)
}

func TestLiveSourceMapsActionableHTTPErrors(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		wantKind   source.ErrorKind
		wantAction bool
	}{
		{name: "401 asks the user to sign in", status: http.StatusUnauthorized, wantKind: source.Unauthorized, wantAction: true},
		{name: "503 is retryable transport", status: http.StatusServiceUnavailable, wantKind: source.Transport},
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
					return []byte(`{"claudeAiOauth":{"accessToken":"right"}}`), nil
				},
			}).Fetch(context.Background())
			var sourceError *source.Error
			if !errors.As(err, &sourceError) || sourceError.Kind != test.wantKind {
				t.Fatalf("LiveSource.Fetch() error = %v, want kind %v", err, test.wantKind)
			}
			if !strings.Contains(strings.ToLower(sourceError.Message), "claude") {
				t.Fatalf("LiveSource.Fetch() message = %q, want Claude", sourceError.Message)
			}
			if test.wantAction && !strings.Contains(sourceError.Message, "run `claude`") {
				t.Fatalf("LiveSource.Fetch() message = %q, want sign-in action", sourceError.Message)
			}
		})
	}
}

func TestSpendBalanceHonoursEnabledState(t *testing.T) {
	tests := []struct {
		name           string
		enabled        bool
		wantHasCredits bool
	}{
		{name: "enabled positive balance is spendable", enabled: true, wantHasCredits: true},
		{name: "disabled positive balance is not spendable", enabled: false, wantHasCredits: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root, err := jsonx.Parse([]byte(`{
				"five_hour":{"utilization":1},
				"spend":{"enabled":` + boolString(test.enabled) + `,"limit":{"amount_minor":2000,"exponent":2},"used":{"amount_minor":500,"exponent":2}}
			}`))
			if err != nil {
				t.Fatal(err)
			}
			provider, ok := Snapshot(root, time.Now(), snapshot.OriginLive, "")
			if !ok || provider.Credits == nil {
				t.Fatalf("Snapshot() = (%#v, %v)", provider, ok)
			}
			if provider.Credits.HasCredits != test.wantHasCredits || provider.Credits.Balance == nil || *provider.Credits.Balance != "15.00" {
				t.Fatalf("Snapshot().Credits = %#v", provider.Credits)
			}
		})
	}
}

func TestDefaultMirrorPathHonoursTheOverrideDirectory(t *testing.T) {
	directory := t.TempDir()
	t.Setenv("QUOTA_MONITOR_DIR", directory)
	if got, want := DefaultMirrorPath(), filepath.Join(directory, "claude-usage.json"); got != want {
		t.Fatalf("DefaultMirrorPath() = %q, want %q", got, want)
	}
}

func assertCanonicalWindows(t *testing.T, windows []snapshot.Window) {
	t.Helper()
	wants := []struct {
		id            string
		label         string
		kind          snapshot.Kind
		usedPercent   float64
		resetsAt      time.Time
		windowMinutes int
	}{
		{id: "session", label: "5h", kind: snapshot.KindSession, usedPercent: 10, resetsAt: time.Date(2026, 8, 29, 18, 59, 59, 0, time.UTC), windowMinutes: 300},
		{id: "weekly_all", label: "Week", kind: snapshot.KindWeekly, usedPercent: 14, resetsAt: time.Date(2026, 8, 31, 13, 59, 59, 0, time.UTC), windowMinutes: 10080},
		{id: "weekly_scoped", label: "Fable wk", kind: snapshot.KindWeekly, usedPercent: 20, resetsAt: time.Date(2026, 8, 31, 13, 59, 59, 0, time.UTC), windowMinutes: 10080},
	}
	if len(windows) != len(wants) {
		t.Fatalf("windows count = %d, want %d: %#v", len(windows), len(wants), windows)
	}
	for index, want := range wants {
		got := windows[index]
		if got.ID != want.id || got.Label != want.label || got.Kind != want.kind || got.UsedPercent != want.usedPercent {
			t.Fatalf("window %d = %#v, want id %q label %q kind %q usage %v", index, got, want.id, want.label, want.kind, want.usedPercent)
		}
		if got.ResetsAt == nil || !got.ResetsAt.UTC().Truncate(time.Second).Equal(want.resetsAt) {
			t.Fatalf("window %d reset = %v, want %s", index, got.ResetsAt, want.resetsAt)
		}
		if got.WindowMinutes == nil || *got.WindowMinutes != want.windowMinutes {
			t.Fatalf("window %d minutes = %v, want %d", index, got.WindowMinutes, want.windowMinutes)
		}
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

func boolString(value bool) string {
	if value {
		return "true"
	}
	return "false"
}
