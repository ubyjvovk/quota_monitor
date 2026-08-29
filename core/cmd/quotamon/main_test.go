package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"quotamon/internal/hybrid"
	"quotamon/internal/registry"
	"quotamon/internal/snapshot"
	"quotamon/internal/source"
)

type commandStubSource struct {
	id          string
	displayName string
	origin      snapshot.Origin
	provider    snapshot.Provider
	err         error
	calls       *atomic.Int32
}

func (s commandStubSource) ProviderID() string      { return s.id }
func (s commandStubSource) DisplayName() string     { return s.displayName }
func (s commandStubSource) Origin() snapshot.Origin { return s.origin }
func (s commandStubSource) Fetch(context.Context) (snapshot.Provider, error) {
	if s.calls != nil {
		s.calls.Add(1)
	}
	return s.provider, s.err
}

func TestSnapshotCommandPrintsFetchedProvidersAndUsesWindowExitStatus(t *testing.T) {
	now := time.Date(2026, 8, 29, 18, 59, 59, 741_925_000, time.UTC)
	provider := commandProvider("claude", "Claude", 43, now.Add(time.Hour))
	configured := []hybrid.Provider{{
		ID: "claude", DisplayName: "Claude",
		Local: commandStubSource{id: "claude", displayName: "Claude", origin: snapshot.OriginLocal, provider: provider},
		Now:   func() time.Time { return now },
	}}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exit := runWithFactory([]string{"snapshot", "--no-live"}, &stdout, &stderr, func() time.Time { return now }, fixedFactory(configured))
	if exit != 0 || stderr.Len() != 0 {
		t.Fatalf("run(snapshot) exit = %d, stderr = %q", exit, stderr.String())
	}

	var object map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &object); err != nil {
		t.Fatalf("snapshot output is not valid JSON: %v", err)
	}
	providers, ok := object["providers"].([]any)
	if !ok || len(providers) != 1 {
		t.Fatalf("providers = %#v, want one provider", object["providers"])
	}
	if object["generatedAt"] != "2026-08-29T18:59:59Z" {
		t.Fatalf("generatedAt = %#v", object["generatedAt"])
	}

	stdout.Reset()
	exit = runWithFactory([]string{"snapshot", "--no-live"}, &stdout, &stderr, func() time.Time { return now }, fixedFactory(nil))
	if exit != 1 {
		t.Fatalf("empty snapshot exit = %d, want 1", exit)
	}
}

func TestWaybarCommandPrintsExactlyOneJSONLineWithFourKeys(t *testing.T) {
	now := time.Date(2026, 8, 29, 18, 59, 59, 0, time.UTC)
	provider := commandProvider("claude", "Claude", 43, now.Add(time.Hour))
	configured := []hybrid.Provider{{
		ID: "claude", DisplayName: "Claude",
		Local: commandStubSource{id: "claude", displayName: "Claude", origin: snapshot.OriginLocal, provider: provider},
		Now:   func() time.Time { return now },
	}}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exit := runWithFactory([]string{"waybar", "--no-live"}, &stdout, &stderr, func() time.Time { return now }, fixedFactory(configured))
	if exit != 0 || stderr.Len() != 0 || strings.Count(stdout.String(), "\n") != 1 {
		t.Fatalf("run(waybar) exit = %d, stdout = %q, stderr = %q", exit, stdout.String(), stderr.String())
	}
	var object map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &object); err != nil {
		t.Fatalf("Waybar output is not valid JSON: %v", err)
	}
	if len(object) != 4 {
		t.Fatalf("Waybar keys = %#v, want four keys", object)
	}
	for _, key := range []string{"text", "tooltip", "class", "percentage"} {
		if _, ok := object[key]; !ok {
			t.Errorf("Waybar output has no %q: %#v", key, object)
		}
	}
}

func TestCheckReportsEachSourceAndAlwaysSucceeds(t *testing.T) {
	plan := "max"
	localProvider := commandProvider("claude", "Claude", 43, time.Now().Add(time.Hour))
	localProvider.Plan = &plan
	configured := []hybrid.Provider{{
		ID: "claude", DisplayName: "Claude",
		Local: commandStubSource{id: "claude", displayName: "Claude", origin: snapshot.OriginLocal, provider: localProvider},
		Live:  commandStubSource{id: "claude", displayName: "Claude", origin: snapshot.OriginLive, err: source.Errorf(source.Unauthorized, "sign in again")},
	}}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exit := runWithFactory([]string{"check"}, &stdout, &stderr, time.Now, fixedFactory(configured))
	if exit != 0 || stderr.Len() != 0 {
		t.Fatalf("run(check) exit = %d, stderr = %q", exit, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Claude local: ok — 1 windows, max") ||
		!strings.Contains(stdout.String(), "Claude live: unauthorized: sign in again") {
		t.Fatalf("check output = %q", stdout.String())
	}
}

func TestNoLiveNeverProbesTheLiveSource(t *testing.T) {
	var liveCalls atomic.Int32
	now := time.Date(2026, 8, 29, 18, 59, 59, 0, time.UTC)
	configured := []hybrid.Provider{{
		ID: "claude", DisplayName: "Claude",
		Local: commandStubSource{id: "claude", displayName: "Claude", origin: snapshot.OriginLocal, provider: commandProvider("claude", "Claude", 43, now.Add(time.Hour))},
		Live:  commandStubSource{id: "claude", displayName: "Claude", origin: snapshot.OriginLive, calls: &liveCalls},
	}}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exit := runWithFactory([]string{"check", "--no-live"}, &stdout, &stderr, func() time.Time { return now }, fixedFactory(configured))
	if exit != 0 || liveCalls.Load() != 0 || strings.Contains(stdout.String(), " live:") {
		t.Fatalf("run(check --no-live) exit = %d, live calls = %d, output = %q", exit, liveCalls.Load(), stdout.String())
	}
}

func TestHelpFormsListEveryCommandOnStandardOutput(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "no arguments", args: nil},
		{name: "long help", args: []string{"--help"}},
		{name: "short help", args: []string{"-h"}},
		{name: "command help", args: []string{"snapshot", "--help"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			exit := run(test.args, &stdout, &stderr, time.Now)
			if exit != 0 || stderr.Len() != 0 {
				t.Fatalf("run() exit = %d, stderr = %q", exit, stderr.String())
			}
			for _, command := range []string{"snapshot", "waybar", "check"} {
				if !strings.Contains(stdout.String(), command) {
					t.Errorf("help does not list %q: %s", command, stdout.String())
				}
			}
		})
	}
}

func TestUnknownCommandsAndFlagsExitTwoWithUsageOnStandardError(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "unknown command", args: []string{"unknown"}},
		{name: "unknown command flag", args: []string{"--unknown"}},
		{name: "unknown subcommand flag", args: []string{"snapshot", "--unknown"}},
		{name: "extra argument", args: []string{"waybar", "extra"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			exit := runWithFactory(test.args, &stdout, &stderr, time.Now, fixedFactory(nil))
			if exit != 2 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "Usage: quotamon") {
				t.Fatalf("run(%q) = exit %d, stdout %q, stderr %q", test.args, exit, stdout.String(), stderr.String())
			}
		})
	}
}

func fixedFactory(configured []hybrid.Provider) providerFactory {
	return func(options registry.Options) []hybrid.Provider {
		providers := append([]hybrid.Provider(nil), configured...)
		for index := range providers {
			providers[index].LiveEnabled = options.LiveEnabled == nil || options.LiveEnabled(providers[index].ID)
		}
		return providers
	}
}

func commandProvider(id, displayName string, percent float64, resetsAt time.Time) snapshot.Provider {
	return snapshot.Provider{
		ID: id, DisplayName: displayName,
		Windows:    []snapshot.Window{{ID: "window", Label: "5h", Kind: snapshot.KindSession, UsedPercent: percent, ResetsAt: &snapshot.Time{Time: resetsAt}}},
		ObservedAt: snapshot.Time{Time: resetsAt.Add(-time.Hour)},
		Origin:     snapshot.OriginLocal, Status: snapshot.OK(),
	}
}
