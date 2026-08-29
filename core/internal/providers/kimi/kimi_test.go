package kimi

import (
	"context"
	"encoding/json"
	"errors"
	"math"
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

func TestParsingFixtureProducesWeeklyAndLimitWindowsInOrder(t *testing.T) {
	root := fixtureRoot(t, "kimi-usages.json")
	observedAt := time.Date(2026, 8, 29, 20, 0, 0, 0, time.UTC)
	provider, ok := Snapshot(root, observedAt)
	if !ok {
		t.Fatal("Snapshot() found no window")
	}

	if provider.ID != ProviderID || provider.DisplayName != DisplayName || provider.Origin != snapshot.OriginLive {
		t.Fatalf("Snapshot() metadata = %#v", provider)
	}
	if provider.Credits != nil {
		t.Fatalf("Snapshot().Credits = %#v, want nil", provider.Credits)
	}
	if provider.Plan == nil || *provider.Plan != "basic" {
		t.Fatalf("Snapshot().Plan = %v, want %q", provider.Plan, "basic")
	}

	if len(provider.Windows) != 2 {
		t.Fatalf("windows count = %d, want 2: %#v", len(provider.Windows), provider.Windows)
	}

	weekly := provider.Windows[0]
	if weekly.ID != "weekly" || weekly.Label != "Week" || weekly.Kind != snapshot.KindWeekly {
		t.Fatalf("weekly window = %#v", weekly)
	}
	if !closePercent(weekly.UsedPercent, 3) {
		t.Fatalf("weekly usedPercent = %v, want 3", weekly.UsedPercent)
	}
	if !snapshotTimeEqual(t, weekly.ResetsAt, "2026-09-04T09:42:34Z") {
		t.Fatalf("weekly reset = %v, want 2026-09-04T09:42:34Z", weekly.ResetsAt)
	}
	if weekly.WindowMinutes == nil || *weekly.WindowMinutes != 10080 {
		t.Fatalf("weekly minutes = %v, want 10080", weekly.WindowMinutes)
	}

	limit := provider.Windows[1]
	if limit.ID != "limit_300m" || limit.Label != "5h" || limit.Kind != snapshot.KindSession {
		t.Fatalf("limit window = %#v", limit)
	}
	if !closePercent(limit.UsedPercent, 14) {
		t.Fatalf("limit usedPercent = %v, want 14", limit.UsedPercent)
	}
	if !snapshotTimeEqual(t, limit.ResetsAt, "2026-08-30T01:42:34Z") {
		t.Fatalf("limit reset = %v, want 2026-08-30T01:42:34Z", limit.ResetsAt)
	}
	if limit.WindowMinutes == nil || *limit.WindowMinutes != 300 {
		t.Fatalf("limit minutes = %v, want 300", limit.WindowMinutes)
	}
}

func TestEncodedSnapshotOmitsTheUserIdIdentifier(t *testing.T) {
	provider, _ := Snapshot(fixtureRoot(t, "kimi-usages.json"), time.Now())
	encoded, err := json.Marshal(provider)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "userId") || strings.Contains(string(encoded), "businessId") {
		t.Fatalf("encoded snapshot carries an identity field: %s", encoded)
	}
}

func TestZeroWeeklyLimitSkipsTheWeeklyWindowButKeepsTheLimitWindow(t *testing.T) {
	body := `{
		"user":{"membership":{"level":"LEVEL_BASIC"}},
		"usage":{"limit":"0","used":"3","resetTime":"2026-09-04T09:42:34.674165Z"},
		"limits":[{"window":{"duration":300,"timeUnit":"TIME_UNIT_MINUTE"},
		           "detail":{"limit":"100","used":"14","remaining":"86","resetTime":"2026-08-30T01:42:34.674165Z"}}]
	}`
	root, err := jsonx.Parse([]byte(body))
	if err != nil {
		t.Fatal(err)
	}

	provider, ok := Snapshot(root, time.Now())
	if !ok {
		t.Fatal("Snapshot() found no window")
	}
	if len(provider.Windows) != 1 || provider.Windows[0].ID != "limit_300m" {
		t.Fatalf("Snapshot().Windows = %#v, want only the limit_300m window", provider.Windows)
	}
}

func TestCredentialParserReadsTheTopLevelAccessToken(t *testing.T) {
	credentials, err := ParseCredentials([]byte(`{"access_token":"tok","refresh_token":"r","expires_at":1788038896,"scope":"x"}`))
	if err != nil {
		t.Fatal(err)
	}
	if credentials.Token != "tok" {
		t.Fatalf("ParseCredentials().Token = %q, want %q", credentials.Token, "tok")
	}
	if credentials.ExpiresAt == nil || credentials.ExpiresAt.Year() != 2026 {
		t.Fatalf("ParseCredentials().ExpiresAt = %v, want year 2026", credentials.ExpiresAt)
	}
}

func TestCredentialParserWithoutATokenAsksTheUserToSignIn(t *testing.T) {
	_, err := ParseCredentials([]byte(`{"refresh_token":"r"}`))
	var sourceError *source.Error
	if !errors.As(err, &sourceError) || sourceError.Kind != source.NotConfigured {
		t.Fatalf("ParseCredentials() error = %v, want NotConfigured", err)
	}
	if !strings.Contains(sourceError.Message, "`kimi`") {
		t.Fatalf("ParseCredentials() message = %q, want a kim sign-in action", sourceError.Message)
	}
}

func TestLiveSourceUsesThePluralUsageEndpointAndBearerHeader(t *testing.T) {
	fixture, err := fixtures.Load("kimi-usages.json")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != "/usages" {
			t.Errorf("request = %s %s, want GET /usages", request.Method, request.URL.Path)
		}
		headers := map[string]string{
			"Authorization": "Bearer tok",
			"Accept":        "application/json",
			"User-Agent":    "quotamon/0.1",
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
			return []byte(`{"access_token":"tok"}`), nil
		},
	}).Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(provider.Windows) != 2 {
		t.Fatalf("windows count = %d, want 2", len(provider.Windows))
	}
}

func TestLiveSourceMapsAuthenticationFailuresToUnauthorized(t *testing.T) {
	tests := []struct {
		name   string
		status int
	}{
		{name: "401 asks the user to run kimi", status: http.StatusUnauthorized},
		{name: "403 asks the user to run kimi", status: http.StatusForbidden},
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
					return []byte(`{"access_token":"tok"}`), nil
				},
			}).Fetch(context.Background())
			var sourceError *source.Error
			if !errors.As(err, &sourceError) || sourceError.Kind != source.Unauthorized {
				t.Fatalf("LiveSource.Fetch() error = %v, want Unauthorized", err)
			}
			if !strings.Contains(sourceError.Message, "kimi") {
				t.Fatalf("LiveSource.Fetch() message = %q, want kimi action", sourceError.Message)
			}
		})
	}
}

func TestLiveSourceReportsMalformedWhenNoWindowCanBeBuilt(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(`{"usage":{},"user":{}}`))
	}))
	defer server.Close()

	_, err := (LiveSource{
		BaseURL: server.URL,
		Client:  server.Client(),
		Credentials: func() ([]byte, error) {
			return []byte(`{"access_token":"tok"}`), nil
		},
	}).Fetch(context.Background())
	var sourceError *source.Error
	if !errors.As(err, &sourceError) || sourceError.Kind != source.Malformed {
		t.Fatalf("LiveSource.Fetch() error = %v, want Malformed", err)
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

func closePercent(got, want float64) bool {
	return math.Abs(got-want) < 1e-9
}

func snapshotTimeEqual(t *testing.T, timestamp *snapshot.Time, want string) bool {
	t.Helper()
	if timestamp == nil {
		return false
	}
	parsed, err := time.Parse(time.RFC3339, want)
	if err != nil {
		t.Fatal(err)
	}
	return timestamp.Truncate(time.Second).Equal(parsed)
}
