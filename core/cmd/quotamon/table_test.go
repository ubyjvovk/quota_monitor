package main

import (
	"testing"
	"time"

	"quotamon/internal/snapshot"
)

func TestRenderTableCoversCurrentAndRolledWindowsCreditsAndUnavailableStatus(t *testing.T) {
	now := time.Date(2026, 8, 29, 19, 0, 0, 0, time.UTC)
	maxPlan := "Max"
	disabledBalance := "3.50"
	monthlySpend := "$7.75 this month"
	input := snapshot.Snapshot{Providers: []snapshot.Provider{
		{
			ID: "claude", DisplayName: "Claude", Plan: &maxPlan,
			Windows: []snapshot.Window{
				{ID: "rolled", Label: "Week", Kind: snapshot.KindWeekly, UsedPercent: 88, ResetsAt: tableTime(now.Add(-time.Minute))},
				{ID: "session", Label: "5h", Kind: snapshot.KindSession, UsedPercent: 42.4, ResetsAt: tableTime(now.Add(time.Hour + 2*time.Minute))},
			},
			Credits:    &snapshot.Credits{Balance: &disabledBalance, Enabled: false},
			ObservedAt: snapshot.Time{Time: now.Add(-3 * time.Minute)},
			Origin:     snapshot.OriginLocal,
			Status:     snapshot.OK(),
		},
		{
			ID: "deepinfra", DisplayName: "DeepInfra",
			Credits:    &snapshot.Credits{Unlimited: true, Balance: &monthlySpend, Enabled: true},
			ObservedAt: snapshot.Time{Time: now.Add(-2 * time.Second)},
			Origin:     snapshot.OriginLive,
			Status:     snapshot.OK(),
		},
		snapshot.Unavailable("grok", "Grok", snapshot.NeedsSetup("set GROK_API_KEY"), now.Add(-2*time.Hour)),
	}}

	want := `Claude          Max    cached · 3m ago
  5h                42%  resets in 1h 2m
  Week                —  window reset
  credits        3.50 (not enabled)
DeepInfra       —      live · just now
  credits        $7.75 this month
Grok            —      unavailable · 2h ago
  !  set GROK_API_KEY`
	if got := renderTable(input, now); got != want {
		t.Fatalf("renderTable() =\n%q\nwant:\n%q", got, want)
	}
}

func tableTime(value time.Time) *snapshot.Time {
	return &snapshot.Time{Time: value}
}
