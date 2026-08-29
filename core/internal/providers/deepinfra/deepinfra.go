// Package deepinfra reads DeepInfra month-to-date spend and prepaid balance
// from its payment API. DeepInfra is pay-as-you-go: there is no quota window
// beyond an optional spending limit, and a percentage exists only when the
// account has that limit set. The account also carries a prepaid balance from
// /payment/checklist; this package never invents a ceiling where none exists.
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

// Balance carries the money/status fields extracted from /payment/checklist.
// Only these fields are read; the response also carries the user's postal
// address and payment method, which must never be retained.
type Balance struct {
	// Stripe is the prepaid balance in USD: negative is funds ready to spend,
	// positive is money owed, zero means no prepaid funds.
	Stripe float64
	// Known reports whether the checklist was read successfully. When false the
	// spend-only fallback is used because there is no balance to trust.
	Known bool
	// Suspended reports whether the account is barred from spending.
	Suspended bool
	// SuspendReason is why the account is suspended, when Suspended is true.
	SuspendReason string
	// OverdueInvoices is how many unpaid invoices exist when > 0.
	OverdueInvoices int
}

// Snapshot normalises DeepInfra config, usage, and checklist readings into one
// provider picture. Beyond a positive spending limit there is no quota to speak
// of, so the percentage window exists only then; otherwise the provider reports
// spend with no percentage rather than a misleading zero.
func Snapshot(limitUSD float64, hasLimit bool, spentUSD float64, periodEnd *time.Time, observedAt time.Time, balance Balance) snapshot.Provider {
	// Spend always reflects usage; it is what the account has actually burnt.
	spend := fmt.Sprintf("$%.2f this month", spentUSD)
	credits := snapshot.Credits{
		HasCredits: false,
		Unlimited:  !hasLimit,
		Balance:    &spend,
		Enabled:    true,
		Spend:      &spend,
	}

	if balance.Known {
		switch {
		case balance.Stripe < 0:
			// Negative stripe_balance is funds ready to spend; report the absolute
			// value as spendable headroom.
			prepaid := fmt.Sprintf("$%.2f", -balance.Stripe)
			credits.HasCredits = true
			credits.Unlimited = false
			credits.Balance = &prepaid
		case balance.Stripe == 0:
			zero := "$0.00"
			credits.HasCredits = false
			credits.Unlimited = false
			credits.Balance = &zero
		default:
			// Positive stripe_balance is money owed to DeepInfra, not headroom.
			owed := fmt.Sprintf("$%.2f owed", balance.Stripe)
			credits.HasCredits = false
			credits.Unlimited = false
			credits.Balance = &owed
		}
		credits.Enabled = !balance.Suspended
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

	status := snapshot.OK()
	if balance.Known {
		switch {
		case balance.Suspended:
			// A suspended account cannot spend; surface why over anything else.
			status = snapshot.Failed("DeepInfra account suspended: " + balance.SuspendReason)
		case balance.OverdueInvoices > 0:
			// Money is owed, so the difference from the owed mapping matters.
			status = snapshot.NeedsSetup(fmt.Sprintf("DeepInfra has $%d in overdue invoices", balance.OverdueInvoices))
		}
	}

	return snapshot.Provider{
		ID:          ProviderID,
		DisplayName: DisplayName,
		Plan:        &plan,
		Windows:     windows,
		Credits:     &credits,
		ObservedAt:  snapshot.Time{Time: observedAt},
		Origin:      snapshot.OriginLive,
		Status:      status,
	}
}
