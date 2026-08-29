package main

import (
	"fmt"
	"strings"
	"time"

	"quotamon/internal/format"
	"quotamon/internal/snapshot"
)

func renderTable(input snapshot.Snapshot, now time.Time) string {
	providers := make([]string, 0, len(input.Providers))
	for _, provider := range input.Providers {
		providers = append(providers, renderTableProvider(provider, now))
	}
	return strings.Join(providers, "\n")
}

func renderTableProvider(provider snapshot.Provider, now time.Time) string {
	plan := "—"
	if provider.Plan != nil {
		plan = *provider.Plan
	}
	lines := []string{fmt.Sprintf(
		"%-15s %-6s %s · %s",
		provider.DisplayName,
		plan,
		tableOrigin(provider.Origin),
		format.Age(now.Sub(provider.ObservedAt.Time)),
	)}

	for _, window := range provider.SortedWindows(now) {
		lines = append(lines, renderTableWindow(window, now))
	}
	if provider.Credits != nil {
		if detail, ok := tableCredits(*provider.Credits); ok {
			lines = append(lines, fmt.Sprintf("  %-14s %s", "credits", detail))
		}
	}
	if provider.Status.State != "ok" {
		lines = append(lines, "  !  "+provider.Status.Message)
	}
	return strings.Join(lines, "\n")
}

func renderTableWindow(window snapshot.Window, now time.Time) string {
	percent := "—"
	if used, ok := window.CurrentUsedPercent(now); ok {
		percent = format.Percent(used)
	}
	line := fmt.Sprintf("  %-14s %6s", window.Label, percent)
	if window.ResetsAt == nil {
		return line
	}
	if remaining := window.ResetsAt.Sub(now); remaining > 0 {
		return line + "  resets in " + format.Countdown(remaining)
	}
	return line + "  window reset"
}

func tableCredits(credits snapshot.Credits) (string, bool) {
	if credits.Unlimited {
		if credits.Balance != nil {
			return *credits.Balance, true
		}
		return "unlimited", true
	}
	if credits.Enabled {
		if credits.Balance != nil {
			return *credits.Balance + " remaining", true
		}
		return "— remaining", true
	}
	if credits.Balance != nil && strings.TrimSpace(*credits.Balance) != "" {
		return *credits.Balance + " (not enabled)", true
	}
	return "", false
}

func tableOrigin(origin snapshot.Origin) string {
	switch origin {
	case snapshot.OriginLive:
		return "live"
	case snapshot.OriginLocal:
		return "cached"
	default:
		return "unavailable"
	}
}
