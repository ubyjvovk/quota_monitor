// Package hybrid resolves live and local sources into one provider snapshot.
package hybrid

import (
	"context"
	"sync"
	"time"

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
	// Now supplies the current time and defaults to time.Now.
	Now func() time.Time
}

type outcome struct {
	provider  snapshot.Provider
	err       error
	attempted bool
}

// Fetch runs the enabled sources concurrently and prefers a usable live reading.
func (p Provider) Fetch(ctx context.Context) snapshot.Provider {
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
		return liveOutcome.provider
	}

	now := p.Now
	if now == nil {
		now = time.Now
	}
	asOf := now()
	if localOutcome.attempted && localOutcome.err == nil && usable(localOutcome.provider) {
		provider := localOutcome.provider
		if len(provider.Windows) > 0 {
			hasCurrentReading := false
			for _, window := range provider.Windows {
				if _, current := window.CurrentUsedPercent(asOf); current {
					hasCurrentReading = true
					break
				}
			}
			if !hasCurrentReading {
				provider.Status = snapshot.NeedsSetup(
					"Last reading " + format.Age(asOf.Sub(provider.ObservedAt.Time)) + "; its window has since reset",
				)
				return provider
			}
		}
		if liveOutcome.attempted && liveOutcome.err != nil {
			provider.Status = snapshot.NeedsSetup("Cached — live refresh failed: " + liveOutcome.err.Error())
		}
		return provider
	}

	message := mostActionableMessage(localOutcome, liveOutcome)
	if message == "" {
		message = "No data source configured"
	}
	return snapshot.Unavailable(p.ID, p.DisplayName, snapshot.NeedsSetup(message), asOf)
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
