package main

import (
	"strings"
	"testing"
	"time"

	"quotamon/internal/snapshot"
)

func TestRenderWaybarUsesEachProvidersTightestReading(t *testing.T) {
	now := time.Date(2026, 8, 29, 20, 0, 0, 0, time.UTC)
	claude := waybarProvider("claude", "Claude", 43, now)
	codex := waybarProvider("codex", "ChatGPT", 18, now)

	got := renderWaybar(snapshot.Snapshot{Providers: []snapshot.Provider{claude, codex}}, now)

	if got.Text != "CL 43% · GPT 18%" || got.Class != "normal" || got.Percentage != 43 {
		t.Fatalf("renderWaybar() = %#v", got)
	}
	if !strings.Contains(got.Tooltip, "Claude · max · live · 2m ago") ||
		!strings.Contains(got.Tooltip, "  Fable wk  43%  resets in 1d 20h") {
		t.Fatalf("tooltip = %q", got.Tooltip)
	}
}

func TestRenderWaybarUsesCriticalClassAtNinetyTwoPercent(t *testing.T) {
	now := time.Date(2026, 8, 29, 20, 0, 0, 0, time.UTC)
	provider := waybarProvider("claude", "Claude", 92, now)

	got := renderWaybar(snapshot.Snapshot{Providers: []snapshot.Provider{provider}}, now)

	if got.Class != "critical" || got.Percentage != 92 {
		t.Fatalf("renderWaybar() = %#v", got)
	}
}

func TestRenderWaybarMarksAProviderWithoutAReadingUnavailable(t *testing.T) {
	now := time.Date(2026, 8, 29, 20, 0, 0, 0, time.UTC)
	provider := waybarProvider("claude", "Claude", 92, now)
	provider.Windows[0].ResetsAt = &snapshot.Time{Time: now}
	provider.Status = snapshot.NeedsSetup("its window has since reset")

	got := renderWaybar(snapshot.Snapshot{Providers: []snapshot.Provider{provider}}, now)

	if got.Text != "CL —" || got.Class != "unavailable" || got.Percentage != 0 {
		t.Fatalf("renderWaybar() = %#v", got)
	}
	if !strings.Contains(got.Tooltip, "  Fable wk  —  window reset") || !strings.Contains(got.Tooltip, "  status  its window has since reset") {
		t.Fatalf("tooltip = %q", got.Tooltip)
	}
}

func TestRenderWaybarUsesTheKimiShortName(t *testing.T) {
	now := time.Date(2026, 8, 29, 20, 0, 0, 0, time.UTC)
	provider := waybarProvider("kimi", "Kimi", 14, now)

	got := renderWaybar(snapshot.Snapshot{Providers: []snapshot.Provider{provider}}, now)

	if got.Text != "KM 14%" {
		t.Fatalf("renderWaybar().Text = %q, want the KM short name", got.Text)
	}
}

func waybarProvider(id, displayName string, percent float64, now time.Time) snapshot.Provider {
	plan := "max"
	minutes := 7 * 24 * 60
	return snapshot.Provider{
		ID: id, DisplayName: displayName, Plan: &plan,
		Windows: []snapshot.Window{{
			ID: "weekly_scoped", Label: "Fable wk", Kind: snapshot.KindWeekly,
			UsedPercent: percent, ResetsAt: &snapshot.Time{Time: now.Add(44 * time.Hour)}, WindowMinutes: &minutes,
		}},
		ObservedAt: snapshot.Time{Time: now.Add(-2 * time.Minute)},
		Origin:     snapshot.OriginLive, Status: snapshot.OK(),
	}
}

func TestRenderWaybarRendersWindowlessDeepInfraCreditsInTooltip(t *testing.T) {
	tests := []struct {
		name    string
		balance string
		want    string
	}{
		{name: "balance", balance: "$7.75 this month", want: "  $7.75 this month"},
		{name: "unlimited", want: "  unlimited"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			now := time.Date(2026, 8, 29, 20, 0, 0, 0, time.UTC)
			provider := waybarProvider("deepinfra", "DeepInfra", 0, now)
			provider.Plan = nil
			provider.Windows = []snapshot.Window{}
			provider.Credits = &snapshot.Credits{Unlimited: true, Enabled: true}
			if test.balance != "" {
				provider.Credits.Balance = &test.balance
			}

			got := renderWaybar(snapshot.Snapshot{Providers: []snapshot.Provider{provider}}, now)

			if got.Text != "DI —" {
				t.Fatalf("renderWaybar().Text = %q, want the DI short name with a dash", got.Text)
			}
			if !strings.Contains(got.Tooltip, "\n"+test.want) {
				t.Fatalf("renderWaybar().Tooltip = %q, want line %q", got.Tooltip, test.want)
			}
		})
	}
}
