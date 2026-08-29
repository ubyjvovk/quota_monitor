// Package format provides compact human-readable formatting shared by providers.
package format

import (
	"math"
	"strconv"
	"time"
)

// Percent rounds a usage value to the compact percentage shown by QuotaKit.
func Percent(value float64) string {
	return strconv.Itoa(int(math.Round(value))) + "%"
}

// Countdown formats a remaining duration like QuotaKit's compact reset display.
func Countdown(duration time.Duration) string {
	total := int(duration / time.Second)
	if total < 0 {
		total = 0
	}
	days := total / (24 * 60 * 60)
	hours := (total % (24 * 60 * 60)) / (60 * 60)
	minutes := (total % (60 * 60)) / 60
	switch {
	case days > 0:
		return strconv.Itoa(days) + "d " + strconv.Itoa(hours) + "h"
	case hours > 0:
		return strconv.Itoa(hours) + "h " + strconv.Itoa(minutes) + "m"
	case minutes > 0:
		return strconv.Itoa(minutes) + "m"
	default:
		return "<1m"
	}
}

// Age formats an elapsed duration like QuotaKit's compact age display.
func Age(duration time.Duration) string {
	total := int(duration / time.Second)
	if total < 0 {
		total = 0
	}
	switch {
	case total < 5:
		return "just now"
	case total < 60:
		return strconv.Itoa(total) + "s ago"
	case total < 60*60:
		return strconv.Itoa(total/60) + "m ago"
	case total < 24*60*60:
		return strconv.Itoa(total/(60*60)) + "h ago"
	default:
		return strconv.Itoa(total/(24*60*60)) + "d ago"
	}
}
