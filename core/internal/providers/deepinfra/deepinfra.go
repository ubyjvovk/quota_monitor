// Package deepinfra reads DeepInfra month-to-date spend from its payment API.
// DeepInfra is pay-as-you-go: there is no quota and no prepaid balance, so the
// honest readout is spend, and a percentage exists only when the account has a
// spending limit set. This package never invents a ceiling where none exists.
package deepinfra

import (
	"fmt"
	"time"

	"quotamon/internal/snapshot"
)

const (
	// ProviderID is DeepInfra's stable provider identifier.
	ProviderID = "deepinfra"
	// DisplayName is DeepInfra's human-readable provider name.
	DisplayName = "DeepInfra"
)

// plan is DeepInfra's subscription class; it has no tiers, only spend.
var plan = "pay-as-you-go"

// Snapshot normalises a DeepInfra config and usage reading into one provider
// picture. Beyond a positive spending limit there is no quota to speak of, so
// the percentage window exists only then; otherwise the provider reports spend
// with no percentage rather than a misleading zero. Credits are always spend,
// never a spendable balance, hence HasCredits is always false.
func Snapshot(limitUSD float64, hasLimit bool, spentUSD float64, periodEnd *time.Time, observedAt time.Time) snapshot.Provider {
	balance := fmt.Sprintf("$%.2f this month", spentUSD)
	credits := snapshot.Credits{
		HasCredits: false,
		Unlimited:  !hasLimit,
		Balance:    &balance,
		Enabled:    true,
	}

	var windows []snapshot.Window
	if hasLimit && limitUSD > 0 {
		var resetsAt *snapshot.Time
		if periodEnd != nil {
			// interval.to is epoch milliseconds; drop the sub-second fraction to
			// honour the snapshot contract, which forbids fractional seconds.
			value := periodEnd.UTC().Truncate(time.Second)
			resetsAt = &snapshot.Time{Time: value}
		}
		windows = []snapshot.Window{{
			ID:            "monthly_spend",
			Label:         "Month",
			Kind:          snapshot.KindMonthly,
			UsedPercent:   spentUSD / limitUSD * 100,
			ResetsAt:      resetsAt,
			WindowMinutes: nil,
		}}
	} else {
		windows = []snapshot.Window{}
	}

	return snapshot.Provider{
		ID:          ProviderID,
		DisplayName: DisplayName,
		Plan:        &plan,
		Windows:     windows,
		Credits:     &credits,
		ObservedAt:  snapshot.Time{Time: observedAt},
		Origin:      snapshot.OriginLive,
		Status:      snapshot.OK(),
	}
}
