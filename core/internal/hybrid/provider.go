// Package hybrid resolves live and local sources into one provider snapshot.
package hybrid

import (
	"context"
	"fmt"
	"sync"
	"time"

	"quotamon/internal/cache"
	"quotamon/internal/format"
	"quotamon/internal/snapshot"
	"quotamon/internal/source"
)

// Provider pairs a provider's local and live quota sources.
type Provider struct {
	// ID is the provider's stable snapshot identifier.
	ID string
	// DisplayName is the provider name shown to users.
	DisplayName string
	// Local is the optional cached or on-disk source.
	Local source.Source
	// Live is the optional endpoint or subprocess source.
	Live source.Source
	// LiveEnabled controls whether Fetch may call Live.
	LiveEnabled bool
	// Cache stores the last usable live reading for stale-token fallback.
	Cache *cache.Store
	// ShortestWindow limits how long a stale-token cache can be returned without refreshing.
	ShortestWindow time.Duration
	// TokenStale reports whether the live credential has expired by timestamp.
	TokenStale func(now time.Time) bool
	// Refresh asks the provider's own client to renew a stale credential.
	Refresh func(ctx context.Context) error
	// Fresh bypasses the stale-token cache and forces a refresh attempt.
	Fresh bool
	// Now supplies the current time and defaults to time.Now.
	Now func() time.Time
}

type outcome struct {
	provider  snapshot.Provider
	err       error
	attempted bool
}

// Fetch uses a current stale-token cache when policy permits, otherwise runs
// the enabled sources concurrently and prefers a usable live reading.
func (p Provider) Fetch(ctx context.Context) snapshot.Provider {
	now := p.Now
	if now == nil {
		now = time.Now
	}
	asOf := now()

	stale := p.LiveEnabled && p.Live != nil && p.TokenStale != nil && p.TokenStale(asOf)
	var cachedOutcome outcome
	if stale && !p.Fresh && p.Cache != nil {
		if provider, found := p.Cache.Load(p.ID); found {
			cachedOutcome = outcome{provider: provider, attempted: true}
			if asOf.Sub(provider.ObservedAt.Time) < p.ShortestWindow && hasCurrentWindow(provider, asOf) {
				return cachedReading(provider, snapshot.OK())
			}
		}
	}

	refreshMessage := ""
	if stale && p.Refresh != nil {
		if err := p.Refresh(ctx); err != nil {
			refreshMessage = fmt.Sprintf("%s sign-in is stale — open `kimi` to refresh it (auto-refresh failed: %v)", p.DisplayName, err)
		}
	}

	var liveOutcome outcome
	var localOutcome outcome
	var group sync.WaitGroup

	attempt := func(candidate source.Source, destination *outcome) {
		defer group.Done()
		destination.attempted = true
		destination.provider, destination.err = candidate.Fetch(ctx)
	}
	if p.LiveEnabled && p.Live != nil {
		group.Add(1)
		go attempt(p.Live, &liveOutcome)
	}
	if p.Local != nil {
		group.Add(1)
		go attempt(p.Local, &localOutcome)
	}
	group.Wait()

	if liveOutcome.attempted && liveOutcome.err == nil && usable(liveOutcome.provider) {
		if p.Cache != nil {
			// Cache persistence is best-effort: a valid live reading should not be
			// hidden merely because its fallback copy could not be written.
			_ = p.Cache.Save(liveOutcome.provider)
		}
		return liveOutcome.provider
	}

	if provider, found := fallbackReading(localOutcome, asOf, refreshMessage, liveOutcome.err, false); found {
		return provider
	}
	if provider, found := fallbackReading(cachedOutcome, asOf, refreshMessage, liveOutcome.err, true); found {
		return provider
	}

	if refreshMessage != "" {
		return snapshot.Unavailable(p.ID, p.DisplayName, snapshot.NeedsSetup(refreshMessage), asOf)
	}

	message := mostActionableMessage(localOutcome, liveOutcome)
	if message == "" {
		message = "No data source configured"
	}
	return snapshot.Unavailable(p.ID, p.DisplayName, snapshot.NeedsSetup(message), asOf)
}

func fallbackReading(candidate outcome, asOf time.Time, refreshMessage string, liveError error, cached bool) (snapshot.Provider, bool) {
	if !candidate.attempted || candidate.err != nil || !usable(candidate.provider) {
		return snapshot.Provider{}, false
	}
	provider := candidate.provider
	if cached {
		provider.Origin = snapshot.OriginLocal
	}
	if len(provider.Windows) > 0 && !hasCurrentWindow(provider, asOf) {
		provider.Status = snapshot.NeedsSetup(
			"Last reading " + format.Age(asOf.Sub(provider.ObservedAt.Time)) + "; its window has since reset",
		)
		return provider, true
	}
	if refreshMessage != "" {
		provider.Status = snapshot.NeedsSetup(refreshMessage)
	} else if liveError != nil {
		provider.Status = snapshot.NeedsSetup("Cached — live refresh failed: " + liveError.Error())
	}
	return provider, true
}

func cachedReading(provider snapshot.Provider, status snapshot.Status) snapshot.Provider {
	provider.Origin = snapshot.OriginLocal
	provider.Status = status
	return provider
}

func hasCurrentWindow(provider snapshot.Provider, now time.Time) bool {
	for _, window := range provider.Windows {
		if _, current := window.CurrentUsedPercent(now); current {
			return true
		}
	}
	return false
}

// usable accepts DeepInfra's pay-as-you-go reading, which has credits but no quota windows.
func usable(p snapshot.Provider) bool {
	return len(p.Windows) > 0 || p.Credits != nil
}

// mostActionableMessage keeps the local error when priorities tie.
func mostActionableMessage(outcomes ...outcome) string {
	message := ""
	priority := -1
	for _, result := range outcomes {
		if !result.attempted || result.err == nil {
			continue
		}
		candidatePriority := source.Priority(result.err)
		if candidatePriority > priority {
			priority = candidatePriority
			message = result.err.Error()
		}
	}
	return message
}
