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

func TestRenderWaybarUsesTheRunInfraShortName(t *testing.T) {
	now := time.Date(2026, 8, 29, 20, 0, 0, 0, time.UTC)
	provider := waybarProvider("runinfra", "RunInfra", 16, now)

	got := renderWaybar(snapshot.Snapshot{Providers: []snapshot.Provider{provider}}, now)

	if got.Text != "RI 16%" {
		t.Fatalf("renderWaybar().Text = %q, want the RI short name", got.Text)
	}
}

func TestProviderTooltipOrdersWindowsLikeTheTable(t *testing.T) {
	now := time.Date(2026, 8, 29, 20, 0, 0, 0, time.UTC)
	provider := waybarProvider("claude", "Claude", 0, now)
	provider.Windows = []snapshot.Window{
		{ID: "weekly", Label: "weekly", Kind: snapshot.KindWeekly, UsedPercent: 40, ResetsAt: &snapshot.Time{Time: now.Add(time.Hour)}},
		{ID: "session", Label: "session", Kind: snapshot.KindSession, UsedPercent: 90, ResetsAt: &snapshot.Time{Time: now.Add(time.Hour)}},
	}

	tooltip := providerTooltip(provider, now)
	if strings.Index(tooltip, "  session  90%") > strings.Index(tooltip, "  weekly  40%") {
		t.Fatalf("providerTooltip() = %q, want the session window first", tooltip)
	}
}

func TestProviderTooltipRendersCachedOrigin(t *testing.T) {
	now := time.Date(2026, 8, 29, 20, 0, 0, 0, time.UTC)
	provider := waybarProvider("claude", "Claude", 43, now)
	provider.Origin = snapshot.OriginLocal

	tooltip := providerTooltip(provider, now)
	if !strings.Contains(tooltip, "Claude · max · cached · 2m ago") {
		t.Fatalf("providerTooltip() = %q, want cached origin", tooltip)
	}
}

func TestProviderTooltipRendersCreditsAlongsideWindows(t *testing.T) {
	now := time.Date(2026, 8, 29, 20, 0, 0, 0, time.UTC)
	provider := waybarProvider("deepinfra", "DeepInfra", 43, now)
	balance := "$10.03"
	provider.Credits = &snapshot.Credits{Balance: &balance, Enabled: true}

	tooltip := providerTooltip(provider, now)
	if !strings.Contains(tooltip, "  credits   $10.03 remaining") {
		t.Fatalf("providerTooltip() = %q, want credits alongside the window", tooltip)
	}
}

func TestRenderWaybarClampsPercentageWithoutChangingTextOrTooltip(t *testing.T) {
	now := time.Date(2026, 8, 29, 20, 0, 0, 0, time.UTC)
	provider := waybarProvider("claude", "Claude", 104, now)

	got := renderWaybar(snapshot.Snapshot{Providers: []snapshot.Provider{provider}}, now)

	if got.Percentage != 100 {
		t.Fatalf("renderWaybar().Percentage = %d, want 100", got.Percentage)
	}
	if !strings.Contains(got.Text, "CL 104%") || !strings.Contains(got.Tooltip, "  Fable wk  104%") {
		t.Fatalf("renderWaybar() = %#v, want unclamped text and tooltip", got)
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
		credits snapshot.Credits
		want    []string
	}{
		{
			name:    "prepaid balance and spend are two lines",
			credits: snapshot.Credits{HasCredits: true, Unlimited: false, Balance: stringPointer("$10.03"), Enabled: true, Spend: stringPointer("$7.75 this month")},
			want:    []string{"  balance   $10.03 remaining", "  spend     $7.75 this month"},
		},
		{
			name:    "spend only keeps a single spend line",
			credits: snapshot.Credits{Unlimited: true, Enabled: true, Balance: stringPointer("$7.75 this month")},
			want:    []string{"  spend     $7.75 this month"},
		},
		{
			name:    "unlimited with no reported balance",
			credits: snapshot.Credits{Unlimited: true, Enabled: true},
			want:    []string{"  credits   unlimited"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			now := time.Date(2026, 8, 29, 20, 0, 0, 0, time.UTC)
			provider := waybarProvider("deepinfra", "DeepInfra", 0, now)
			provider.Plan = nil
			provider.Windows = []snapshot.Window{}
			provider.Credits = &test.credits

			got := renderWaybar(snapshot.Snapshot{Providers: []snapshot.Provider{provider}}, now)

			if got.Text != "DI —" {
				t.Fatalf("renderWaybar().Text = %q, want the DI short name with a dash", got.Text)
			}
			for _, want := range test.want {
				if !strings.Contains(got.Tooltip, "\n"+want) {
					t.Fatalf("renderWaybar().Tooltip = %q, want line %q", got.Tooltip, want)
				}
			}
		})
	}
}

func stringPointer(value string) *string { return &value }
