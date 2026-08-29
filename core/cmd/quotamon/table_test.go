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

func TestTableCreditsOmitsOnlyEmptyOrZeroDisabledBalances(t *testing.T) {
	disabledZero := "0"
	disabledDecimalZero := "0.00"
	disabledCommaZero := " € 0,00 "
	disabledNonzero := "20.00"
	enabledZero := "0"

	tests := []struct {
		name    string
		credits snapshot.Credits
		want    string
		wantOK  bool
	}{
		{
			name:    "disabled zero has no line",
			credits: snapshot.Credits{Balance: &disabledZero},
		},
		{
			name:    "disabled decimal zero has no line",
			credits: snapshot.Credits{Balance: &disabledDecimalZero},
		},
		{
			name:    "disabled comma zero with currency and spaces has no line",
			credits: snapshot.Credits{Balance: &disabledCommaZero},
		},
		{
			name:    "disabled nonzero balance is labelled not enabled",
			credits: snapshot.Credits{Balance: &disabledNonzero},
			want:    "20.00 (not enabled)",
			wantOK:  true,
		},
		{
			name:    "disabled nil balance has no line",
			credits: snapshot.Credits{},
		},
		{
			name:    "enabled zero balance remains visible",
			credits: snapshot.Credits{Balance: &enabledZero, Enabled: true},
			want:    "0 remaining",
			wantOK:  true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := tableCredits(test.credits)
			if got != test.want || ok != test.wantOK {
				t.Fatalf("tableCredits() = %q, %v; want %q, %v", got, ok, test.want, test.wantOK)
			}
		})
	}
}

func tableTime(value time.Time) *snapshot.Time {
	return &snapshot.Time{Time: value}
}
