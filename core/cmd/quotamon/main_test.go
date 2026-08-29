package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestSnapshotCommandPrintsAnEmptyValidSnapshot(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	wantTime := time.Date(2026, 8, 29, 18, 59, 59, 741_925_000, time.UTC)
	exit := run([]string{"snapshot"}, &stdout, &stderr, func() time.Time { return wantTime })
	if exit != 0 || stderr.Len() != 0 {
		t.Fatalf("run(snapshot) exit = %d, stderr = %q", exit, stderr.String())
	}

	var object map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &object); err != nil {
		t.Fatalf("snapshot output is not valid JSON: %v", err)
	}
	providers, ok := object["providers"].([]any)
	if !ok || len(providers) != 0 {
		t.Fatalf("providers = %#v, want empty array", object["providers"])
	}
	if object["generatedAt"] != "2026-08-29T18:59:59Z" {
		t.Fatalf("generatedAt = %#v", object["generatedAt"])
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

func TestIncompleteAndUnknownCommandsExitTwoOnStandardError(t *testing.T) {
	tests := []struct {
		name     string
		command  string
		wantText string
	}{
		{name: "waybar is not implemented", command: "waybar", wantText: "waybar: not implemented"},
		{name: "check is not implemented", command: "check", wantText: "check: not implemented"},
		{name: "unknown command prints usage", command: "unknown", wantText: "Usage: quotamon"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			exit := run([]string{test.command}, &stdout, &stderr, time.Now)
			if exit != 2 || stdout.Len() != 0 || !strings.Contains(stderr.String(), test.wantText) {
				t.Fatalf("run(%q) = exit %d, stdout %q, stderr %q", test.command, exit, stdout.String(), stderr.String())
			}
		})
	}
}
