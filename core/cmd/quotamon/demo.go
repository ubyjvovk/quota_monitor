package main

import (
	"time"

	"quotamon/internal/snapshot"
)

// demoSnapshot keeps documentation output representative without exposing a
// real account. Every timestamp derives from base so render tests and captured
// countdowns stay stable while normal invocations still describe "just now".
func demoSnapshot(base time.Time) snapshot.Snapshot {
	base = base.UTC().Truncate(time.Second)
	maxPlan := "max"
	plusPlan := "plus"
	payAsYouGoPlan := "pay-as-you-go"
	basicPlan := "basic"
	claudeBalance := "20.00"
	deepInfraBalance := "$10.03"
	deepInfraSpend := "$8.00 this month"

	return snapshot.Snapshot{
		Providers: []snapshot.Provider{
			{
				ID: "claude", DisplayName: "Claude", Plan: &maxPlan,
				Windows: []snapshot.Window{
					demoWindow("session", "5h", snapshot.KindSession, 6, base.Add(2*time.Hour+39*time.Minute)),
					demoWindow("weekly_all", "Week", snapshot.KindWeekly, 15, base.Add(40*time.Hour)),
					demoWindow("weekly_scoped", "Fable wk", snapshot.KindWeekly, 23, base.Add(40*time.Hour)),
				},
				Credits:    &snapshot.Credits{HasCredits: true, Balance: &claudeBalance},
				ObservedAt: snapshot.Time{Time: base},
				Origin:     snapshot.OriginLive,
				Status:     snapshot.OK(),
			},
			{
				ID: "codex", DisplayName: "ChatGPT", Plan: &plusPlan,
				Windows: []snapshot.Window{
					demoWindow("session", "5h", snapshot.KindSession, 100, base.Add(8*time.Minute)),
					demoWindow("weekly", "Week", snapshot.KindWeekly, 31, base.Add(5*24*time.Hour+11*time.Hour)),
				},
				ObservedAt: snapshot.Time{Time: base},
				Origin:     snapshot.OriginLive,
				Status:     snapshot.OK(),
			},
			{
				ID: "grok", DisplayName: "Grok",
				Windows: []snapshot.Window{
					demoWindow("weekly", "Week", snapshot.KindWeekly, 63, base.Add(2*24*time.Hour+13*time.Hour)),
				},
				ObservedAt: snapshot.Time{Time: base},
				Origin:     snapshot.OriginLive,
				Status:     snapshot.OK(),
			},
			{
				ID: "deepinfra", DisplayName: "DeepInfra", Plan: &payAsYouGoPlan,
				Windows: []snapshot.Window{},
				Credits: &snapshot.Credits{
					HasCredits: true,
					Balance:    &deepInfraBalance,
					Enabled:    true,
					Spend:      &deepInfraSpend,
				},
				ObservedAt: snapshot.Time{Time: base},
				Origin:     snapshot.OriginLive,
				Status:     snapshot.OK(),
			},
			{
				ID: "kimi", DisplayName: "Kimi", Plan: &basicPlan,
				Windows: []snapshot.Window{
					demoWindow("session", "5h", snapshot.KindSession, 42, base.Add(3*time.Hour+12*time.Minute)),
					demoWindow("weekly", "Week", snapshot.KindWeekly, 14, base.Add(4*24*time.Hour+6*time.Hour)),
				},
				ObservedAt: snapshot.Time{Time: base},
				Origin:     snapshot.OriginLive,
				Status:     snapshot.OK(),
			},
		},
		GeneratedAt: snapshot.Time{Time: base},
	}
}

func demoWindow(id, label string, kind snapshot.Kind, usedPercent float64, resetsAt time.Time) snapshot.Window {
	return snapshot.Window{
		ID:          id,
		Label:       label,
		Kind:        kind,
		UsedPercent: usedPercent,
		ResetsAt:    &snapshot.Time{Time: resetsAt},
	}
}
