package main

import (
	"math"
	"strings"
	"time"

	"quotamon/internal/format"
	"quotamon/internal/snapshot"
)

type waybarPayload struct {
	Text       string `json:"text"`
	Tooltip    string `json:"tooltip"`
	Class      string `json:"class"`
	Percentage int    `json:"percentage"`
}

func renderWaybar(input snapshot.Snapshot, now time.Time) waybarPayload {
	textParts := make([]string, 0, len(input.Providers))
	tooltipProviders := make([]string, 0, len(input.Providers))
	headlinePercent := 0.0
	hasHeadline := false

	for _, provider := range input.Providers {
		shortName := providerShortName(provider)
		if window, ok := provider.TightestWindow(now); ok {
			used, _ := window.CurrentUsedPercent(now)
			textParts = append(textParts, shortName+" "+format.Percent(used))
			if !hasHeadline || used > headlinePercent {
				headlinePercent = used
				hasHeadline = true
			}
		} else {
			textParts = append(textParts, shortName+" —")
		}
		tooltipProviders = append(tooltipProviders, providerTooltip(provider, now))
	}

	text := strings.Join(textParts, " · ")
	if text == "" {
		text = "Quota —"
	}
	payload := waybarPayload{
		Text:       text,
		Tooltip:    strings.Join(tooltipProviders, "\n"),
		Class:      "unavailable",
		Percentage: 0,
	}
	if hasHeadline {
		payload.Class = severity(headlinePercent)
		payload.Percentage = int(math.Round(headlinePercent))
	}
	return payload
}

func providerTooltip(provider snapshot.Provider, now time.Time) string {
	plan := "—"
	if provider.Plan != nil {
		plan = *provider.Plan
	}
	lines := []string{
		provider.DisplayName + " · " + plan + " · " + string(provider.Origin) + " · " + format.Age(now.Sub(provider.ObservedAt.Time)),
	}
	for _, window := range provider.Windows {
		used := "—"
		if current, ok := window.CurrentUsedPercent(now); ok {
			used = format.Percent(current)
		}
		reset := "reset time unknown"
		if window.ResetsAt != nil {
			if remaining := window.ResetsAt.Sub(now); remaining > 0 {
				reset = "resets in " + format.Countdown(remaining)
			} else {
				reset = "window reset"
			}
		}
		lines = append(lines, "  "+window.Label+"  "+used+"  "+reset)
	}
	if len(provider.Windows) == 0 && provider.Credits != nil {
		// A windowless credits provider renders its credit rows the same way the
		// table does, so the waybar tooltip and the table never disagree.
		lines = append(lines, creditLines(*provider.Credits)...)
	}
	if provider.Status.State != "ok" {
		status := provider.Status.Message
		if status == "" {
			status = provider.Status.State
		}
		lines = append(lines, "  status  "+status)
	}
	return strings.Join(lines, "\n")
}

func providerShortName(provider snapshot.Provider) string {
	switch provider.ID {
	case "claude":
		return "CL"
	case "codex":
		return "GPT"
	case "grok":
		return "GK"
	case "deepinfra":
		return "DI"
	case "kimi":
		return "KM"
	default:
		return provider.DisplayName
	}
}

func severity(percent float64) string {
	switch {
	case percent < 70:
		return "normal"
	case percent < 90:
		return "warning"
	default:
		return "critical"
	}
}
