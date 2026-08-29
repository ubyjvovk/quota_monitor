package main

import (
	"math"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"quotamon/internal/format"
	"quotamon/internal/snapshot"
)

func renderTable(input snapshot.Snapshot, now time.Time) string {
	return renderTableWithColor(input, now, false)
}

func renderTableWithColor(input snapshot.Snapshot, now time.Time, colorEnabled bool) string {
	providers := make([]string, 0, len(input.Providers))
	for _, provider := range input.Providers {
		providers = append(providers, renderTableProvider(provider, now, colorEnabled))
	}
	return strings.Join(providers, "\n\n")
}

func renderTableProvider(provider snapshot.Provider, now time.Time, colorEnabled bool) string {
	plan := "—"
	if provider.Plan != nil {
		plan = *provider.Plan
	}
	lines := []string{
		padTableCell(provider.DisplayName, 12) + " " +
			padTableCell(plan, 14) + " " +
			tableOrigin(provider.Origin) + " · " + format.Age(now.Sub(provider.ObservedAt.Time)),
	}

	for _, window := range provider.SortedWindows(now) {
		lines = append(lines, renderTableWindow(window, now, colorEnabled))
	}
	if provider.Credits != nil {
		lines = append(lines, creditLines(*provider.Credits)...)
	}
	if provider.Status.State != "ok" {
		lines = append(lines, "  !  "+provider.Status.Message)
	}
	return strings.Join(lines, "\n")
}

func renderTableWindow(window snapshot.Window, now time.Time, colorEnabled bool) string {
	percent := "—"
	used, hasReading := window.CurrentUsedPercent(now)
	if hasReading {
		percent = format.Percent(used)
	}

	bar := renderTableBar(used, hasReading, colorEnabled)
	percentPadding := 4 - utf8.RuneCountInString(percent)
	if percentPadding < 0 {
		percentPadding = 0
	}
	if colorEnabled && hasReading {
		percent = format.Colorize(severity(used), percent)
	}

	return "  " + truncateAndPadTableCell(window.Label, 9) + " " +
		bar + " " + strings.Repeat(" ", percentPadding) + percent + "  " +
		tableCountdown(window, now)
}

func renderTableBar(used float64, hasReading, colorEnabled bool) string {
	filled := 0
	if hasReading {
		filled = int(math.Round(used / 5))
		if filled < 0 {
			filled = 0
		}
		if filled > 20 {
			filled = 20
		}
	}

	filledCells := strings.Repeat("█", filled)
	if colorEnabled && hasReading {
		filledCells = format.Colorize(severity(used), filledCells)
	}
	return filledCells + strings.Repeat("░", 20-filled)
}

func tableCountdown(window snapshot.Window, now time.Time) string {
	if window.ResetsAt == nil {
		return "—"
	}
	remaining := window.ResetsAt.Sub(now)
	if remaining <= 0 {
		return "reset"
	}
	return format.Countdown(remaining)
}

func padTableCell(value string, width int) string {
	padding := width - utf8.RuneCountInString(value)
	if padding <= 0 {
		return value
	}
	return value + strings.Repeat(" ", padding)
}

func truncateAndPadTableCell(value string, width int) string {
	if utf8.RuneCountInString(value) > width {
		runes := []rune(value)
		value = string(runes[:width-1]) + "…"
	}
	return padTableCell(value, width)
}

// creditLines renders the provider credit rows shared by the table and the
// waybar tooltip. A distinct prepaid balance and month-to-date spend appear as
// two rows (balance, then spend); a provider that reports only spend keeps the
// single spend row so the balance-derived spend is never doubled.
func creditLines(credits snapshot.Credits) []string {
	if credits.Spend != nil && !credits.Unlimited {
		lines := []string{}
		if detail, ok := tableCredits(credits); ok {
			lines = append(lines, "  "+padTableCell("balance", 9)+" "+detail)
		}
		lines = append(lines, "  "+padTableCell("spend", 9)+" "+*credits.Spend)
		return lines
	}
	if detail, ok := tableCredits(credits); ok {
		label := "credits"
		if credits.Unlimited && credits.Balance != nil {
			label = "spend"
		}
		return []string{"  " + padTableCell(label, 9) + " " + detail}
	}
	return nil
}

// tableCredits omits disabled credits whose balance is absent, blank, or
// numerically zero after surrounding currency symbols and spaces are trimmed.
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
	if credits.Balance == nil || disabledCreditBalanceIsEmptyOrZero(*credits.Balance) {
		return "", false
	}
	return *credits.Balance + " (not enabled)", true
}

func disabledCreditBalanceIsEmptyOrZero(balance string) bool {
	trimmed := strings.TrimFunc(balance, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.Is(unicode.Sc, r)
	})
	if trimmed == "" {
		return true
	}

	amount, err := strconv.ParseFloat(strings.ReplaceAll(trimmed, ",", "."), 64)
	return err == nil && amount == 0
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
