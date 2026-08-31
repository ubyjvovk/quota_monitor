// Package openrouter reads OpenRouter prepaid credits and lifetime usage from
// its documented credits API. OpenRouter is live-only: there is no local file
// source, and the endpoint reports money rather than quota windows.
package openrouter

import (
	"fmt"
	"time"

	"quotamon/internal/snapshot"
)

const (
	// ProviderID is OpenRouter's stable provider identifier.
	ProviderID = "openrouter"
	// DisplayName is OpenRouter's human-readable provider name.
	DisplayName = "OpenRouter"
)

// Snapshot normalises OpenRouter's total purchased credits and lifetime usage
// into one provider picture. Remaining credit is floored at zero because usage
// can exceed the purchased total, while Spend remains the API's lifetime total.
func Snapshot(totalCredits, totalUsage float64, observedAt time.Time) snapshot.Provider {
	remaining := totalCredits - totalUsage
	if remaining < 0 {
		remaining = 0
	}
	balance := fmt.Sprintf("$%.2f", remaining)
	spend := fmt.Sprintf("$%.2f all time", totalUsage)
	credits := snapshot.Credits{
		HasCredits: remaining > 0,
		Unlimited:  false,
		Enabled:    true,
		Balance:    &balance,
		Spend:      &spend,
	}

	return snapshot.Provider{
		ID:          ProviderID,
		DisplayName: DisplayName,
		Plan:        nil,
		Windows:     []snapshot.Window{},
		Credits:     &credits,
		ObservedAt:  snapshot.Time{Time: observedAt},
		Origin:      snapshot.OriginLive,
		Status:      snapshot.OK(),
	}
}
