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

// earlyServeMaxAge is one refresh interval and matches a token's practical
// lifetime, so skipping live cannot leave a provider frozen for hours or days.
const earlyServeMaxAge = 15 * time.Minute

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
	// Cache stores the last usable live reading for fallback and early serving.
	Cache *cache.Store
	// ShortestWindow records the provider's shortest quota window.
	ShortestWindow time.Duration
	// TokenStale reports whether the live credential has expired by timestamp.
	TokenStale func(now time.Time) bool
	// Refresh asks the provider's own client to renew a stale credential.
	Refresh func(ctx context.Context) error
	// Fresh bypasses stale-token early serving and forces a refresh attempt.
	Fresh bool
	// Now supplies the current time and defaults to time.Now.
	Now func() time.Time
}

type outcome struct {
	provider  snapshot.Provider
	err       error
	attempted bool
}

// Prepared is a Provider whose stale-token policy has already run: PreFetch
// consulted the cache and attempted any credential refresh, so Fetch only
// queries the sources. Splitting the phases lets callers give the (possibly
// process-launching) refresh its own budget instead of the fetch timeout.
type Prepared struct {
	provider       Provider
	asOf           time.Time
	cachedOutcome  outcome
	refreshMessage string
	served         snapshot.Provider
	servedReady    bool
}

// PreFetch resolves the stale-token policy without querying any source. It
// loads the fallback cache first; a very young reading with a current window
// can short-circuit a stale token, otherwise the provider's own refresh runs.
// Disabling live sources still prevents refreshes and live queries.
func (p Provider) PreFetch(ctx context.Context) Prepared {
	now := p.Now
	if now == nil {
		now = time.Now
	}
	asOf := now()
	prepared := Prepared{provider: p, asOf: asOf}
	if p.Cache != nil {
		if cached, found := p.Cache.Load(p.ID); found {
			prepared.cachedOutcome = outcome{provider: cached, attempted: true}
		}
	}

	stale := p.LiveEnabled && p.Live != nil && p.TokenStale != nil && p.TokenStale(asOf)
	if !stale {
		return prepared
	}
	if !p.Fresh && prepared.cachedOutcome.attempted {
		cached := prepared.cachedOutcome.provider
		if asOf.Sub(cached.ObservedAt.Time) < earlyServeMaxAge && hasCurrentWindow(cached, asOf) {
			prepared.served = cachedReading(cached, snapshot.OK())
			prepared.servedReady = true
			return prepared
		}
	}
	if p.Refresh != nil {
		if err := p.Refresh(ctx); err != nil {
			prepared.refreshMessage = fmt.Sprintf("%s sign-in is stale — open `kimi` to refresh it (auto-refresh failed: %v)", p.DisplayName, err)
		}
	}
	return prepared
}

// Fetch uses a very young stale-token cache when policy permits, otherwise it
// runs the enabled sources and keeps the cache as a live-failure fallback. It
// is PreFetch and Prepared.Fetch in one step, for callers that do not separate
// the refresh budget from the fetch budget.
func (p Provider) Fetch(ctx context.Context) snapshot.Provider {
	return p.PreFetch(ctx).Fetch(ctx)
}

// Fetch runs the enabled sources concurrently and prefers a usable live
// reading, falling back to the local and then the cached reading.
func (prepared Prepared) Fetch(ctx context.Context) snapshot.Provider {
	if prepared.servedReady {
		return prepared.served
	}
	p := prepared.provider
	asOf := prepared.asOf

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

	if provider, found := fallbackReading(localOutcome, asOf, prepared.refreshMessage, liveOutcome.err, false); found {
		return provider
	}
	if liveOutcome.attempted {
		if provider, found := fallbackReading(prepared.cachedOutcome, asOf, prepared.refreshMessage, liveOutcome.err, true); found {
			return provider
		}
	}

	if prepared.refreshMessage != "" {
		return snapshot.Unavailable(p.ID, p.DisplayName, snapshot.NeedsSetup(prepared.refreshMessage), asOf)
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
	if provider.Status.State == "needsSetup" {
		return provider, true
	}
	if provider.Status.State == "ok" && len(provider.Windows) > 0 && !hasCurrentWindow(provider, asOf) {
		provider.Status = snapshot.NeedsSetup(
			"Last reading " + format.Age(asOf.Sub(provider.ObservedAt.Time)) + "; its window has since reset",
		)
		return provider, true
	}
	if refreshMessage != "" {
		if cached {
			refreshMessage = cachedFallbackMessage(provider, refreshMessage, asOf)
		}
		provider.Status = snapshot.NeedsSetup(refreshMessage)
	} else if liveError != nil {
		message := "Cached — live refresh failed: " + liveError.Error()
		if cached {
			message = cachedFallbackMessage(provider, "live refresh failed: "+liveError.Error(), asOf)
		}
		provider.Status = snapshot.NeedsSetup(message)
	}
	return provider, true
}

func cachedFallbackMessage(provider snapshot.Provider, message string, asOf time.Time) string {
	return "Cached " + format.Age(asOf.Sub(provider.ObservedAt.Time)) + " — " + message
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
