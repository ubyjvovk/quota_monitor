// Package deepseek reads DeepSeek account balance from its documented API.
// DeepSeek is live-only: there is no local file source, and the endpoint
// reports money rather than quota windows or spend.
package deepseek

import (
	"fmt"
	"time"

	"quotamon/internal/snapshot"
)

const (
	// ProviderID is DeepSeek's stable provider identifier.
	ProviderID = "deepseek"
	// DisplayName is DeepSeek's human-readable provider name.
	DisplayName = "DeepSeek"
)

// Snapshot normalises DeepSeek's selected currency balance and availability
// flag into one provider picture. USD uses a dollar prefix; all other
// currencies use the amount followed by the API's currency code.
func Snapshot(currency string, totalBalance float64, available bool, observedAt time.Time) snapshot.Provider {
	balance := fmt.Sprintf("%.2f %s", totalBalance, currency)
	if currency == "USD" {
		balance = fmt.Sprintf("$%.2f", totalBalance)
	}
	credits := snapshot.Credits{
		HasCredits: available && totalBalance > 0,
		Unlimited:  false,
		Enabled:    true,
		Balance:    &balance,
		Spend:      nil,
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
