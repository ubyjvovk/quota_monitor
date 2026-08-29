package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"quotamon/internal/fixtures"
	"quotamon/internal/snapshot"
	"quotamon/internal/source"
)

func TestParseAppServerOutputSelectsTheExplicitRateLimitsReply(t *testing.T) {
	fixture := compactFixture(t, "codex-app-server-ratelimits.json")
	stdout := bytes.Join([][]byte{
		[]byte(`{"id":1,"result":{}}`),
		[]byte(`{"method":"remoteControl/status/changed","params":{"rateLimitsByLimitId":{"decoy":true}}}`),
		fixture,
	}, []byte("\n"))

	limits, err := ParseAppServerOutput(stdout)
	if err != nil {
		t.Fatal(err)
	}
	provider, ok := Snapshot(limits, time.Unix(0, 0), snapshot.OriginLive)
	if !ok {
		t.Fatal("Snapshot() rejected fixture limits")
	}
	assertAppServerFixture(t, provider)
}

func TestParseAppServerOutputReportsReplyErrorsAndMissingReplies(t *testing.T) {
	tests := []struct {
		name     string
		stdout   string
		wantKind source.ErrorKind
		wantText string
	}{
		{name: "id two errors are transport failures", stdout: `{"id":2,"error":{"message":"boom"}}`, wantKind: source.Transport, wantText: "boom"},
		{name: "missing id two is malformed", stdout: "junk\n{\"id\":1,\"result\":{}}", wantKind: source.Malformed, wantText: "id:2"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ParseAppServerOutput([]byte(test.stdout))
			var sourceError *source.Error
			if !errors.As(err, &sourceError) {
				t.Fatalf("error = %T %v, want *source.Error", err, err)
			}
			if sourceError.Kind != test.wantKind || !strings.Contains(sourceError.Error(), test.wantText) {
				t.Fatalf("error = (%v, %q), want kind %v containing %q", sourceError.Kind, sourceError.Error(), test.wantKind, test.wantText)
			}
		})
	}
}

func TestRunAppServerKeepsStdinOpenUntilRateLimitsReply(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake codex CLI is a shell script")
	}

	fixture := compactFixture(t, "codex-app-server-ratelimits.json")
	directory := t.TempDir()
	script := "#!/bin/sh\n" +
		"set -eu\n" +
		"printf '%s\\n' '{\"id\":1,\"result\":{}}'\n" +
		"IFS= read -r first_request\n" +
		"IFS= read -r second_request\n" +
		"IFS= read -r third_request\n" +
		"cat <<'QUOTAMON_FIXTURE'\n" +
		string(fixture) + "\n" +
		"QUOTAMON_FIXTURE\n" +
		"cat >/dev/null\n"
	path := filepath.Join(directory, "codex")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH"))

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	started := time.Now()
	output, err := runAppServer(ctx, []byte(appServerRequests))
	elapsed := time.Since(started)
	if err != nil {
		t.Fatal(err)
	}
	if elapsed >= 3*time.Second {
		t.Fatalf("runAppServer returned after %s, want less than 3s", elapsed)
	}
	if !bytes.Contains(output, fixture) {
		t.Fatalf("output = %q, want id:2 fixture", output)
	}
}

func TestAppServerSourceUsesTheInjectedExchangeAndReturnsLiveQuota(t *testing.T) {
	fixture := compactFixture(t, "codex-app-server-ratelimits.json")
	wantRequests := "{\"method\":\"initialize\",\"id\":1,\"params\":{\"clientInfo\":{\"name\":\"quotamon\",\"title\":\"Quota Monitor\",\"version\":\"0.1.0\"}}}\n" +
		"{\"method\":\"initialized\"}\n" +
		"{\"method\":\"account/rateLimits/read\",\"id\":2}\n"
	sourceUnderTest := AppServerSource{Run: func(_ context.Context, requests []byte) ([]byte, error) {
		if got := string(requests); got != wantRequests {
			t.Fatalf("requests = %q, want %q", got, wantRequests)
		}
		return fixture, nil
	}}

	provider, err := sourceUnderTest.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if provider.Origin != snapshot.OriginLive {
		t.Fatalf("origin = %q, want live", provider.Origin)
	}
	assertAppServerFixture(t, provider)
}

func TestLocalSourceReadsTheLastRolloutRecordAndLabelsRollover(t *testing.T) {
	tests := []struct {
		name      string
		now       time.Time
		wantState string
	}{
		{name: "a current window keeps status ok", now: time.Date(2026, 7, 31, 19, 32, 0, 0, time.UTC), wantState: "ok"},
		{name: "all rolled windows request another turn", now: time.Date(2028, 7, 31, 19, 32, 0, 0, time.UTC), wantState: "needsSetup"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			home := rolloutHome(t)
			provider, err := (LocalSource{Home: home, Now: func() time.Time { return test.now }}).Fetch(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if provider.Origin != snapshot.OriginLocal || provider.Status.State != test.wantState {
				t.Fatalf("origin/status = %q/%q, want local/%q", provider.Origin, provider.Status.State, test.wantState)
			}
			if test.wantState == "needsSetup" && !strings.Contains(provider.Status.Message, "after a Codex turn") {
				t.Fatalf("status message = %q", provider.Status.Message)
			}
			if got := provider.ObservedAt.UTC().Truncate(time.Second); !got.Equal(time.Date(2026, 7, 31, 19, 31, 13, 0, time.UTC)) {
				t.Fatalf("observedAt = %s", provider.ObservedAt.Time)
			}
			if provider.Plan == nil || *provider.Plan != "plus" {
				t.Fatalf("plan = %#v, want plus", provider.Plan)
			}
			if len(provider.Windows) != 2 {
				t.Fatalf("windows = %#v", provider.Windows)
			}
			primary, secondary := provider.Windows[0], provider.Windows[1]
			if primary.ID != "primary" || primary.UsedPercent != 18 || primary.Label != "Week" || primary.WindowMinutes == nil || *primary.WindowMinutes != 10080 {
				t.Fatalf("primary = %#v", primary)
			}
			if secondary.ID != "secondary" || secondary.UsedPercent != 42.5 || secondary.Label != "5h" || secondary.WindowMinutes == nil || *secondary.WindowMinutes != 300 {
				t.Fatalf("secondary = %#v", secondary)
			}
		})
	}
}

func TestLastLineEscalatesWithoutReturningAPartialLeadingFragment(t *testing.T) {
	data, err := fixtures.Load("codex-rollout.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	want := lines[len(lines)-1]
	got, ok, err := LastLine(path, "rate_limits", 24)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || got != want {
		t.Fatalf("LastLine() = (%q, %v), want final whole record", got, ok)
	}
	if !json.Valid([]byte(got)) {
		t.Fatalf("LastLine() returned a partial JSON fragment: %q", got)
	}
}

func TestHTTPSourceMapsWhamUsageAndIdentifiesHonestly(t *testing.T) {
	type requestHeaders struct {
		originator string
		userAgent  string
		authorize  string
		accountID  string
	}
	headers := make(chan requestHeaders, 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		headers <- requestHeaders{
			originator: request.Header.Get("originator"),
			userAgent:  request.Header.Get("User-Agent"),
			authorize:  request.Header.Get("Authorization"),
			accountID:  request.Header.Get("ChatGPT-Account-Id"),
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"plan_type":"plus","rate_limit":{"primary_window":{"used_percent":26,"limit_window_seconds":18000,"reset_at":1788038896}},"user_id":"discard-me","email":"discard-me@example.com"}`))
	}))
	defer server.Close()

	home := t.TempDir()
	auth := []byte(`{"tokens":{"access_token":"test-token","account_id":"test-account"}}`)
	if err := os.WriteFile(filepath.Join(home, "auth.json"), auth, 0o600); err != nil {
		t.Fatal(err)
	}
	provider, err := (HTTPSource{Home: home, Endpoint: server.URL, Client: server.Client()}).Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	request := <-headers
	if request.originator != "" {
		t.Fatalf("originator = %q, want absent", request.originator)
	}
	if !strings.HasPrefix(request.userAgent, "quotamon") {
		t.Fatalf("User-Agent = %q, want quotamon prefix", request.userAgent)
	}
	if request.authorize != "Bearer test-token" || request.accountID != "test-account" {
		t.Fatalf("credential headers = %q/%q", request.authorize, request.accountID)
	}
	if len(provider.Windows) != 1 {
		t.Fatalf("windows = %#v", provider.Windows)
	}
	window := provider.Windows[0]
	if window.UsedPercent != 26 || window.WindowMinutes == nil || *window.WindowMinutes != 300 || window.Label != "5h" {
		t.Fatalf("primary window = %#v", window)
	}
}

func compactFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := fixtures.Load(name)
	if err != nil {
		t.Fatal(err)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, data); err != nil {
		t.Fatal(err)
	}
	return compact.Bytes()
}

func assertAppServerFixture(t *testing.T, provider snapshot.Provider) {
	t.Helper()
	if provider.Plan == nil || *provider.Plan != "plus" {
		t.Fatalf("plan = %#v, want plus", provider.Plan)
	}
	if len(provider.Windows) != 2 {
		t.Fatalf("windows = %#v, want primary and secondary", provider.Windows)
	}
	primary, secondary := provider.Windows[0], provider.Windows[1]
	if primary.ID != "primary" || primary.Label != "5h" || primary.Kind != snapshot.KindSession || primary.UsedPercent != 24 || primary.WindowMinutes == nil || *primary.WindowMinutes != 300 {
		t.Fatalf("primary = %#v", primary)
	}
	if primary.ResetsAt == nil || primary.ResetsAt.UTC().Format(time.RFC3339) != "2026-08-29T21:28:16Z" {
		t.Fatalf("primary reset = %#v", primary.ResetsAt)
	}
	if secondary.ID != "secondary" || secondary.Label != "Week" || secondary.Kind != snapshot.KindWeekly || secondary.UsedPercent != 20 || secondary.WindowMinutes == nil || *secondary.WindowMinutes != 10080 {
		t.Fatalf("secondary = %#v", secondary)
	}
	if secondary.ResetsAt == nil || !secondary.ResetsAt.Equal(time.Unix(1788511023, 0)) {
		t.Fatalf("secondary reset = %#v", secondary.ResetsAt)
	}
	if provider.Credits == nil || provider.Credits.HasCredits || provider.Credits.Unlimited || provider.Credits.Enabled || provider.Credits.Balance == nil || *provider.Credits.Balance != "0" {
		t.Fatalf("credits = %#v", provider.Credits)
	}
}

func rolloutHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	directory := filepath.Join(home, "sessions", "2026", "07", "31")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	data, err := fixtures.Load("codex-rollout.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "rollout-fixture.jsonl"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	return home
}
