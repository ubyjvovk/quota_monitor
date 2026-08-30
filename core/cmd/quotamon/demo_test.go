package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"quotamon/internal/discover"
	"quotamon/internal/hybrid"
	"quotamon/internal/registry"
	"quotamon/internal/snapshot"
)

func TestDemoRendersAStableRepresentativeTableThroughTheNormalRenderer(t *testing.T) {
	now := time.Date(2026, 8, 29, 19, 0, 0, 0, time.UTC)
	input := demoSnapshot(now)
	want := `Claude       max            live · just now
  Fable wk  █████░░░░░░░░░░░░░░░  23%  1d 16h
  Week      ███░░░░░░░░░░░░░░░░░  15%  1d 16h
  5h        █░░░░░░░░░░░░░░░░░░░   6%  2h 39m
  credits   20.00 (not enabled)

ChatGPT      plus           live · just now
  5h        ████████████████████ 100%  8m
  Week      ██████░░░░░░░░░░░░░░  31%  5d 11h

Grok         —              live · just now
  Week      █████████████░░░░░░░  63%  2d 13h

DeepInfra    pay-as-you-go  live · just now
  balance   $10.03 remaining
  spend     $8.00 this month

Kimi         basic          live · just now
  5h        ████████░░░░░░░░░░░░  42%  3h 12m
  Week      ███░░░░░░░░░░░░░░░░░  14%  4d 6h`

	if got := renderTable(input, now); got != want {
		t.Fatalf("renderTable(demoSnapshot()) =\n%q\nwant:\n%q", got, want)
	}
}

func TestDemoJSONAndWaybarBypassConfigAndProviderDiscovery(t *testing.T) {
	t.Setenv("QUOTA_MONITOR_DIR", t.TempDir())
	now := time.Date(2026, 8, 29, 19, 0, 0, 0, time.UTC)
	providers := func(registry.Options) []hybrid.Provider {
		t.Fatal("demo called the provider factory")
		return nil
	}
	findings := func() []discover.Finding {
		t.Fatal("demo probed provider discovery")
		return nil
	}

	for _, test := range []struct {
		name string
		args []string
	}{
		{name: "JSON alias contains five providers", args: []string{"--demo", "--json"}},
		{name: "snapshot command contains five providers", args: []string{"snapshot", "--demo"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			exit := runWithDependencies(test.args, strings.NewReader(""), &stdout, &stderr, func() time.Time { return now }, providers, findings)
			if exit != 0 || stderr.Len() != 0 {
				t.Fatalf("run(%q) exit = %d, stderr = %q", test.args, exit, stderr.String())
			}
			var got snapshot.Snapshot
			if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
				t.Fatalf("run(%q) is not valid snapshot JSON: %v", test.args, err)
			}
			if len(got.Providers) != 5 {
				t.Fatalf("run(%q) providers = %d, want 5", test.args, len(got.Providers))
			}
		})
	}

	t.Run("default table renders without config", func(t *testing.T) {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		exit := runWithDependencies([]string{"--demo", "--color=never"}, strings.NewReader(""), &stdout, &stderr, func() time.Time { return now }, providers, findings)
		if exit != 0 || stderr.Len() != 0 {
			t.Fatalf("--demo exit = %d, stderr = %q", exit, stderr.String())
		}
		if !strings.Contains(stdout.String(), "Claude") || !strings.Contains(stdout.String(), "$10.03 remaining") {
			t.Fatalf("--demo table output = %q", stdout.String())
		}
	})

	t.Run("Waybar is exactly one JSON line", func(t *testing.T) {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		exit := runWithDependencies([]string{"waybar", "--demo"}, strings.NewReader(""), &stdout, &stderr, func() time.Time { return now }, providers, findings)
		if exit != 0 || stderr.Len() != 0 {
			t.Fatalf("waybar --demo exit = %d, stderr = %q", exit, stderr.String())
		}
		if strings.Count(stdout.String(), "\n") != 1 {
			t.Fatalf("waybar --demo output = %q, want one line", stdout.String())
		}
		var got waybarPayload
		if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
			t.Fatalf("waybar --demo is not valid JSON: %v", err)
		}
	})
}

func TestDemoTableStillHonoursColorAndNoColor(t *testing.T) {
	t.Setenv("QUOTA_MONITOR_DIR", t.TempDir())
	now := time.Date(2026, 8, 29, 19, 0, 0, 0, time.UTC)
	runDemo := func(t *testing.T) string {
		t.Helper()
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		if exit := run([]string{"--demo", "--color=always"}, strings.NewReader(""), &stdout, &stderr, func() time.Time { return now }); exit != 0 {
			t.Fatalf("--demo --color=always exit = %d, stderr = %q", exit, stderr.String())
		}
		return stdout.String()
	}

	unsetEnvironment(t, "NO_COLOR")
	if got := runDemo(t); !strings.Contains(got, "\x1b[31m") {
		t.Fatalf("colored demo has no critical ANSI sequence: %q", got)
	}
	t.Setenv("NO_COLOR", "1")
	if got := runDemo(t); strings.Contains(got, "\x1b") {
		t.Fatalf("NO_COLOR demo contains ANSI: %q", got)
	}
}
