// Package runinfra reads RunInfra prepaid credits and monthly spend from its
// credits API. RunInfra is live-only: there is no local file source. Credits
// are prepaid, and admission checks the *available* balance — never the ledger
// balance or the transient held funds. A percentage window exists only when
// the account has set a hard monthly spend cap, because a soft or absent cap
// is advisory and must not read as quota pressure.
package runinfra

import (
	"fmt"
	"time"

	"quotamon/internal/snapshot"
)

const (
	// ProviderID is RunInfra's stable provider identifier.
	ProviderID = "runinfra"
	// DisplayName is RunInfra's human-readable provider name.
	DisplayName = "RunInfra"
)

// Snapshot normalises RunInfra plan, prepaid credits, monthly spend, and an
// optional hard spend cap into one provider picture. RunInfra always reports
// prepaid credits and month-to-date spend; the percentage window appears only
// when a hard monthly cap is set, because a soft or absent cap is advisory and
// must not read as quota pressure.
func Snapshot(plan string, availableCents int, spentCents int, capUsedPercent float64, hasHardCap bool, observedAt time.Time) snapshot.Provider {
	var planPtr *string
	if plan != "" {
		planPtr = &plan
	}

	// available_cents is the headroom admission checks (the dashboard's
	// spendable balance, distinct from the ledger balance_cents and any held
	// funds), shown as the headline next to month-to-date spend.
	balance := fmt.Sprintf("$%.2f", float64(availableCents)/100)
	spend := fmt.Sprintf("$%.2f this month", float64(spentCents)/100)
	credits := snapshot.Credits{
		HasCredits: availableCents > 0,
		Unlimited:  false,
		Enabled:    true,
		Balance:    &balance,
		Spend:      &spend,
	}

	var windows []snapshot.Window
	if hasHardCap {
		windows = []snapshot.Window{{
			ID:            "monthly_cap",
			Label:         "Cap",
			Kind:          snapshot.KindMonthly,
			UsedPercent:   capUsedPercent,
			ResetsAt:      nil,
			WindowMinutes: nil,
		}}
	} else {
		// No window for a soft or absent cap: gates_inference is false, so a
		// soft cap is advisory and must not read as quota pressure, and the API
		// reports no period end for the cap to reset on.
		windows = []snapshot.Window{}
	}

	return snapshot.Provider{
		ID:          ProviderID,
		DisplayName: DisplayName,
		Plan:        planPtr,
		Windows:     windows,
		Credits:     &credits,
		ObservedAt:  snapshot.Time{Time: observedAt},
		Origin:      snapshot.OriginLive,
		Status:      snapshot.OK(),
	}
}
