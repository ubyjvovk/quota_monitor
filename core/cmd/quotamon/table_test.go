package main

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"quotamon/internal/snapshot"
)

func TestRenderTableMatchesTheAlignedGoldenLayout(t *testing.T) {
	now := time.Date(2026, 8, 29, 19, 0, 0, 0, time.UTC)
	maxPlan := "max"
	plusPlan := "plus"
	payAsYouGoPlan := "pay-as-you-go"
	disabledBalance := "20.00"
	monthlySpend := "$7.96 this month"
	input := snapshot.Snapshot{Providers: []snapshot.Provider{
		{
			ID: "claude", DisplayName: "Claude", Plan: &maxPlan,
			Windows: []snapshot.Window{
				{ID: "fave", Label: "Fave wk", Kind: snapshot.KindWeekly, UsedPercent: 23, ResetsAt: tableTime(now.Add(40 * time.Hour))},
				{ID: "week", Label: "Week", Kind: snapshot.KindWeekly, UsedPercent: 15, ResetsAt: tableTime(now.Add(40 * time.Hour))},
				{ID: "session", Label: "5h", Kind: snapshot.KindSession, UsedPercent: 6, ResetsAt: tableTime(now.Add(2*time.Hour + 39*time.Minute))},
			},
			Credits:    &snapshot.Credits{Balance: &disabledBalance, Enabled: false},
			ObservedAt: snapshot.Time{Time: now},
			Origin:     snapshot.OriginLive,
			Status:     snapshot.OK(),
		},
		{
			ID: "codex", DisplayName: "ChatGPT", Plan: &plusPlan,
			Windows: []snapshot.Window{
				{ID: "session", Label: "5h", Kind: snapshot.KindSession, UsedPercent: 100, ResetsAt: tableTime(now.Add(8 * time.Minute))},
				{ID: "week", Label: "Week", Kind: snapshot.KindWeekly, UsedPercent: 31, ResetsAt: tableTime(now.Add(5*24*time.Hour + 11*time.Hour))},
			},
			ObservedAt: snapshot.Time{Time: now},
			Origin:     snapshot.OriginLive,
			Status:     snapshot.OK(),
		},
		{
			ID: "grok", DisplayName: "Grok",
			Windows:    []snapshot.Window{{ID: "week", Label: "Week", Kind: snapshot.KindWeekly, UsedPercent: 63, ResetsAt: tableTime(now.Add(2*24*time.Hour + 13*time.Hour))}},
			ObservedAt: snapshot.Time{Time: now}, Origin: snapshot.OriginLive, Status: snapshot.OK(),
		},
		{
			ID: "deepinfra", DisplayName: "DeepInfra", Plan: &payAsYouGoPlan,
			Credits:    &snapshot.Credits{Unlimited: true, Balance: &monthlySpend, Enabled: true},
			ObservedAt: snapshot.Time{Time: now}, Origin: snapshot.OriginLive, Status: snapshot.OK(),
		},
	}}

	want := `Claude       max            live · just now
  Fave wk   █████░░░░░░░░░░░░░░░  23%  1d 16h
  Week      ███░░░░░░░░░░░░░░░░░  15%  1d 16h
  5h        █░░░░░░░░░░░░░░░░░░░   6%  2h 39m
  credits   20.00 (not enabled)

ChatGPT      plus           live · just now
  5h        ████████████████████ 100%  8m
  Week      ██████░░░░░░░░░░░░░░  31%  5d 11h

Grok         —              live · just now
  Week      █████████████░░░░░░░  63%  2d 13h

DeepInfra    pay-as-you-go  live · just now
  spend     $7.96 this month`
	if got := renderTable(input, now); got != want {
		t.Fatalf("renderTable() =\n%q\nwant:\n%q", got, want)
	}

	percentColumn := -1
	for _, line := range strings.Split(want, "\n") {
		percentEnd := strings.Index(line, "%")
		if percentEnd < 0 {
			continue
		}
		column := utf8.RuneCountInString(line[:percentEnd+1]) - 4
		if percentColumn == -1 {
			percentColumn = column
		} else if column != percentColumn {
			t.Fatalf("percent column starts at rune %d in %q, want %d", column, line, percentColumn)
		}
	}
}

func TestRenderTableWindowShowsResetAndTruncatesLabelsByRunes(t *testing.T) {
	now := time.Date(2026, 8, 29, 19, 0, 0, 0, time.UTC)
	window := snapshot.Window{
		ID: "rolled", Label: "一二三四五六七八九十", Kind: snapshot.KindWeekly,
		UsedPercent: 88, ResetsAt: tableTime(now.Add(-time.Minute)),
	}
	want := "  一二三四五六七八… ░░░░░░░░░░░░░░░░░░░░    —  reset"
	if got := renderTableWindow(window, now, false); got != want {
		t.Fatalf("renderTableWindow() = %q, want %q", got, want)
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
