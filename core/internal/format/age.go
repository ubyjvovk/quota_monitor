// Package format provides compact human-readable formatting shared by providers.
package format

import (
	"strconv"
	"time"
)

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
