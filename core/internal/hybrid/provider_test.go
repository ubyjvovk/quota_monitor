package hybrid_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"quotamon/internal/cache"
	"quotamon/internal/hybrid"
	"quotamon/internal/snapshot"
	"quotamon/internal/source"
)

type stubSource struct {
	origin   snapshot.Origin
	provider snapshot.Provider
	err      error
	calls    *atomic.Int32
}

func (s stubSource) ProviderID() string      { return "test" }
func (s stubSource) DisplayName() string     { return "Test" }
func (s stubSource) Origin() snapshot.Origin { return s.origin }
func (s stubSource) Fetch(context.Context) (snapshot.Provider, error) {
	if s.calls != nil {
		s.calls.Add(1)
	}
	return s.provider, s.err
}

func TestFetchPrefersAUsableLiveReading(t *testing.T) {
	now := time.Date(2026, 8, 29, 20, 0, 0, 0, time.UTC)
	live := providerWithWindow(snapshot.OriginLive, now.Add(time.Hour), 43)
	local := providerWithWindow(snapshot.OriginLocal, now.Add(time.Hour), 18)

	got := (hybrid.Provider{
		ID: "test", DisplayName: "Test",
		Local: localSource(local), Live: liveSource(live), LiveEnabled: true,
		Now: func() time.Time { return now },
	}).Fetch(context.Background())

	if got.Origin != snapshot.OriginLive || got.Windows[0].UsedPercent != 43 {
		t.Fatalf("Fetch() = origin %q usage %v, want live 43", got.Origin, got.Windows[0].UsedPercent)
	}
}

func TestFetchPrefersAUsableLiveCreditsReading(t *testing.T) {
	now := time.Date(2026, 8, 29, 20, 0, 0, 0, time.UTC)
	live := providerWithCredits(snapshot.OriginLive, now)
	local := providerWithWindow(snapshot.OriginLocal, now.Add(time.Hour), 18)

	got := (hybrid.Provider{
		ID: "test", DisplayName: "Test",
		Local: localSource(local), Live: liveSource(live), LiveEnabled: true,
		Now: func() time.Time { return now },
	}).Fetch(context.Background())

	if !reflect.DeepEqual(got, live) || got.Status.State != "ok" {
		t.Fatalf("Fetch() = %#v, want unchanged live credits provider %#v", got, live)
	}
}

func TestFetchLabelsLocalReadingWhenLiveFails(t *testing.T) {
	now := time.Date(2026, 8, 29, 20, 0, 0, 0, time.UTC)
	local := providerWithWindow(snapshot.OriginLocal, now.Add(time.Hour), 18)
	liveError := source.Errorf(source.Transport, "endpoint unavailable")

	got := (hybrid.Provider{
		ID: "test", DisplayName: "Test",
		Local: localSource(local), Live: stubSource{origin: snapshot.OriginLive, err: liveError}, LiveEnabled: true,
		Now: func() time.Time { return now },
	}).Fetch(context.Background())

	if got.Origin != snapshot.OriginLocal || got.Status.State != "needsSetup" ||
		!strings.Contains(got.Status.Message, "Cached — live refresh failed: endpoint unavailable") {
		t.Fatalf("Fetch() = origin %q status %#v", got.Origin, got.Status)
	}
}

func TestFetchLabelsLocalCreditsReadingWhenLiveFails(t *testing.T) {
	now := time.Date(2026, 8, 29, 20, 0, 0, 0, time.UTC)
	local := providerWithCredits(snapshot.OriginLocal, now)
	liveError := source.Errorf(source.Transport, "endpoint unavailable")

	got := (hybrid.Provider{
		ID: "test", DisplayName: "Test",
		Local: localSource(local), Live: stubSource{origin: snapshot.OriginLive, err: liveError}, LiveEnabled: true,
		Now: func() time.Time { return now },
	}).Fetch(context.Background())

	want := local
	want.Status = snapshot.NeedsSetup("Cached — live refresh failed: endpoint unavailable")
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Fetch() = %#v, want %#v", got, want)
	}
}

func TestFetchTreatsAResultWithoutWindowsOrCreditsAsNothing(t *testing.T) {
	now := time.Date(2026, 8, 29, 20, 0, 0, 0, time.UTC)
	empty := snapshot.Provider{
		ID: "test", DisplayName: "Test", ObservedAt: snapshot.Time{Time: now},
		Origin: snapshot.OriginLive, Status: snapshot.OK(),
	}

	got := (hybrid.Provider{
		ID: "test", DisplayName: "Test",
		Live: liveSource(empty), LiveEnabled: true,
		Now: func() time.Time { return now },
	}).Fetch(context.Background())

	if got.Origin != snapshot.OriginUnavailable || got.Status.State != "needsSetup" || got.Status.Message != "No data source configured" {
		t.Fatalf("Fetch() = origin %q status %#v", got.Origin, got.Status)
	}
}

func TestFetchReportsRolloverAheadOfALiveFailure(t *testing.T) {
	now := time.Date(2026, 8, 29, 20, 0, 0, 0, time.UTC)
	local := providerWithWindow(snapshot.OriginLocal, now.Add(-time.Minute), 18)
	local.ObservedAt = snapshot.Time{Time: now.Add(-2 * time.Hour)}

	got := (hybrid.Provider{
		ID: "test", DisplayName: "Test",
		Local:       localSource(local),
		Live:        stubSource{origin: snapshot.OriginLive, err: source.Errorf(source.Transport, "endpoint unavailable")},
		LiveEnabled: true,
		Now:         func() time.Time { return now },
	}).Fetch(context.Background())

	if got.Status.State != "needsSetup" || got.Status.Message != "Last reading 2h ago; its window has since reset" {
		t.Fatalf("Fetch().Status = %#v", got.Status)
	}
}

func TestFetchSurfacesTheHighestPriorityFailure(t *testing.T) {
	now := time.Date(2026, 8, 29, 20, 0, 0, 0, time.UTC)
	got := (hybrid.Provider{
		ID: "test", DisplayName: "Test",
		Local:       stubSource{origin: snapshot.OriginLocal, err: source.Errorf(source.NotConfigured, "install local helper")},
		Live:        stubSource{origin: snapshot.OriginLive, err: source.Errorf(source.Unauthorized, "sign in again")},
		LiveEnabled: true,
		Now:         func() time.Time { return now },
	}).Fetch(context.Background())

	if got.Origin != snapshot.OriginUnavailable || got.Status.State != "needsSetup" || got.Status.Message != "sign in again" {
		t.Fatalf("Fetch() = origin %q status %#v", got.Origin, got.Status)
	}
}

func TestFetchDoesNotCallLiveWhenItIsDisabled(t *testing.T) {
	var liveCalls atomic.Int32
	now := time.Date(2026, 8, 29, 20, 0, 0, 0, time.UTC)
	local := providerWithWindow(snapshot.OriginLocal, now.Add(time.Hour), 18)
	got := (hybrid.Provider{
		ID: "test", DisplayName: "Test",
		Local:       localSource(local),
		Live:        stubSource{origin: snapshot.OriginLive, provider: providerWithWindow(snapshot.OriginLive, now.Add(time.Hour), 43), calls: &liveCalls},
		LiveEnabled: false,
		Now:         func() time.Time { return now },
	}).Fetch(context.Background())

	if liveCalls.Load() != 0 || got.Origin != snapshot.OriginLocal || got.Status.State != "ok" {
		t.Fatalf("live calls = %d, provider origin/status = %q/%q", liveCalls.Load(), got.Origin, got.Status.State)
	}
}

func TestCachePolicy(t *testing.T) {
	now := time.Date(2026, 8, 29, 20, 0, 0, 0, time.UTC)
	tests := []struct {
		name             string
		cacheAge         time.Duration
		tokenStale       bool
		liveError        error
		providerStatus   string
		wantOrigin       snapshot.Origin
		wantStatus       string
		wantMessage      string
		wantUsage        float64
		wantLiveCalls    int32
		wantRefreshCalls int32
	}{
		{
			name:          "outage with valid token serves cache with age label",
			cacheAge:      time.Minute,
			liveError:     source.Errorf(source.Transport, "endpoint unavailable"),
			wantOrigin:    snapshot.OriginLocal,
			wantStatus:    "needsSetup",
			wantMessage:   "Cached 1m ago — live refresh failed: endpoint unavailable",
			wantUsage:     18,
			wantLiveCalls: 1,
		},
		{
			name:             "stale token with three hour cache attempts live",
			cacheAge:         3 * time.Hour,
			tokenStale:       true,
			wantOrigin:       snapshot.OriginLive,
			wantStatus:       "ok",
			wantUsage:        43,
			wantLiveCalls:    1,
			wantRefreshCalls: 1,
		},
		{
			name:          "stale token with ten minute cache serves early",
			cacheAge:      10 * time.Minute,
			tokenStale:    true,
			wantOrigin:    snapshot.OriginLocal,
			wantStatus:    "ok",
			wantUsage:     18,
			wantLiveCalls: 0,
		},
		{
			name:           "provider needs setup status survives rollover fallback",
			liveError:      source.Errorf(source.Transport, "endpoint unavailable"),
			providerStatus: "Run a Codex turn to record fresh usage",
			wantOrigin:     snapshot.OriginLocal,
			wantStatus:     "needsSetup",
			wantMessage:    "Run a Codex turn to record fresh usage",
			wantUsage:      18,
			wantLiveCalls:  1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var store *cache.Store
			if test.cacheAge > 0 {
				candidate := cache.Store{Dir: t.TempDir()}
				cached := providerWithWindow(snapshot.OriginLive, now.Add(4*time.Hour), 18)
				cached.ObservedAt = snapshot.Time{Time: now.Add(-test.cacheAge)}
				if err := candidate.Save(cached); err != nil {
					t.Fatal(err)
				}
				store = &candidate
			}

			var local source.Source
			if test.providerStatus != "" {
				provider := providerWithWindow(snapshot.OriginLocal, now.Add(-time.Minute), 18)
				provider.ObservedAt = snapshot.Time{Time: now.Add(-2 * time.Hour)}
				provider.Status = snapshot.NeedsSetup(test.providerStatus)
				local = localSource(provider)
			}

			var liveCalls atomic.Int32
			var refreshCalls atomic.Int32
			got := (hybrid.Provider{
				ID: "test", DisplayName: "Test",
				Local:          local,
				Live:           stubSource{origin: snapshot.OriginLive, provider: providerWithWindow(snapshot.OriginLive, now.Add(time.Hour), 43), err: test.liveError, calls: &liveCalls},
				LiveEnabled:    true,
				Cache:          store,
				ShortestWindow: 5 * time.Hour,
				TokenStale:     func(time.Time) bool { return test.tokenStale },
				Refresh: func(context.Context) error {
					refreshCalls.Add(1)
					return nil
				},
				Now: func() time.Time { return now },
			}).Fetch(context.Background())

			if got.Origin != test.wantOrigin || got.Status.State != test.wantStatus || got.Status.Message != test.wantMessage || got.Windows[0].UsedPercent != test.wantUsage {
				t.Fatalf("Fetch() = origin/status/message/usage %q/%q/%q/%v, want %q/%q/%q/%v", got.Origin, got.Status.State, got.Status.Message, got.Windows[0].UsedPercent, test.wantOrigin, test.wantStatus, test.wantMessage, test.wantUsage)
			}
			if liveCalls.Load() != test.wantLiveCalls || refreshCalls.Load() != test.wantRefreshCalls {
				t.Fatalf("live/refresh calls = %d/%d, want %d/%d", liveCalls.Load(), refreshCalls.Load(), test.wantLiveCalls, test.wantRefreshCalls)
			}
		})
	}
}

func TestRefreshFailureReturnsTheCachedReadingWithAKimiAction(t *testing.T) {
	now := time.Date(2026, 8, 29, 20, 0, 0, 0, time.UTC)
	store := cache.Store{Dir: t.TempDir()}
	cached := providerWithWindow(snapshot.OriginLive, now.Add(time.Hour), 18)
	cached.ObservedAt = snapshot.Time{Time: now.Add(-6 * time.Hour)}
	if err := store.Save(cached); err != nil {
		t.Fatal(err)
	}
	var liveCalls atomic.Int32
	var refreshCalls atomic.Int32

	got := (hybrid.Provider{
		ID: "test", DisplayName: "Kimi",
		Live:           stubSource{origin: snapshot.OriginLive, err: source.Errorf(source.Unauthorized, "stale token rejected"), calls: &liveCalls},
		LiveEnabled:    true,
		Cache:          &store,
		ShortestWindow: 5 * time.Hour,
		TokenStale:     func(time.Time) bool { return true },
		Refresh: func(context.Context) error {
			refreshCalls.Add(1)
			return errors.New("pty unavailable")
		},
		Now: func() time.Time { return now },
	}).Fetch(context.Background())

	if got.Origin != snapshot.OriginLocal || got.Windows[0].UsedPercent != 18 || got.Status.State != "needsSetup" {
		t.Fatalf("Fetch() = %#v, want cached reading with needsSetup", got)
	}
	if !strings.Contains(got.Status.Message, "open `kimi`") || !strings.Contains(got.Status.Message, "pty unavailable") {
		t.Fatalf("Fetch().Status.Message = %q, want Kimi action and refresh error", got.Status.Message)
	}
	if refreshCalls.Load() != 1 || liveCalls.Load() != 1 {
		t.Fatalf("refresh/live calls = %d/%d, want 1/1", refreshCalls.Load(), liveCalls.Load())
	}
}

func TestFreshBypassesAYoungCacheAndRefreshesBeforeLive(t *testing.T) {
	now := time.Date(2026, 8, 29, 20, 0, 0, 0, time.UTC)
	store := cache.Store{Dir: t.TempDir()}
	if err := store.Save(providerWithWindow(snapshot.OriginLive, now.Add(time.Hour), 18)); err != nil {
		t.Fatal(err)
	}
	var liveCalls atomic.Int32
	var refreshCalls atomic.Int32

	got := (hybrid.Provider{
		ID: "test", DisplayName: "Test",
		Live:           stubSource{origin: snapshot.OriginLive, provider: providerWithWindow(snapshot.OriginLive, now.Add(time.Hour), 43), calls: &liveCalls},
		LiveEnabled:    true,
		Cache:          &store,
		ShortestWindow: 5 * time.Hour,
		TokenStale:     func(time.Time) bool { return true },
		Refresh: func(context.Context) error {
			refreshCalls.Add(1)
			return nil
		},
		Fresh: true,
		Now:   func() time.Time { return now },
	}).Fetch(context.Background())

	if got.Origin != snapshot.OriginLive || got.Windows[0].UsedPercent != 43 || refreshCalls.Load() != 1 || liveCalls.Load() != 1 {
		t.Fatalf("Fetch() = %#v with refresh/live calls %d/%d, want fresh live reading", got, refreshCalls.Load(), liveCalls.Load())
	}
}

func TestFreshTokenIgnoresCacheCallsLiveAndSavesTheResult(t *testing.T) {
	now := time.Date(2026, 8, 29, 20, 0, 0, 0, time.UTC)
	store := cache.Store{Dir: t.TempDir()}
	if err := store.Save(providerWithWindow(snapshot.OriginLive, now.Add(time.Hour), 18)); err != nil {
		t.Fatal(err)
	}
	var liveCalls atomic.Int32
	live := providerWithWindow(snapshot.OriginLive, now.Add(time.Hour), 43)

	got := (hybrid.Provider{
		ID: "test", DisplayName: "Test",
		Live:           stubSource{origin: snapshot.OriginLive, provider: live, calls: &liveCalls},
		LiveEnabled:    true,
		Cache:          &store,
		ShortestWindow: 5 * time.Hour,
		TokenStale:     func(time.Time) bool { return false },
		Now:            func() time.Time { return now },
	}).Fetch(context.Background())

	saved, ok := store.Load("test")
	if got.Windows[0].UsedPercent != 43 || liveCalls.Load() != 1 || !ok || saved.Windows[0].UsedPercent != 43 {
		t.Fatalf("Fetch() usage/calls/cache = %v/%d/%#v, want 43/1/saved live", got.Windows[0].UsedPercent, liveCalls.Load(), saved)
	}
}

func TestPreFetchRefreshesOnceBeforeASeparateFetch(t *testing.T) {
	now := time.Date(2026, 8, 29, 20, 0, 0, 0, time.UTC)
	store := cache.Store{Dir: t.TempDir()}
	cached := providerWithWindow(snapshot.OriginLive, now.Add(time.Hour), 18)
	cached.ObservedAt = snapshot.Time{Time: now.Add(-6 * time.Hour)}
	if err := store.Save(cached); err != nil {
		t.Fatal(err)
	}
	var liveCalls atomic.Int32
	var refreshCalls atomic.Int32

	provider := hybrid.Provider{
		ID: "test", DisplayName: "Test",
		Live:           stubSource{origin: snapshot.OriginLive, provider: providerWithWindow(snapshot.OriginLive, now.Add(time.Hour), 43), calls: &liveCalls},
		LiveEnabled:    true,
		Cache:          &store,
		ShortestWindow: 5 * time.Hour,
		TokenStale:     func(time.Time) bool { return true },
		Refresh: func(context.Context) error {
			refreshCalls.Add(1)
			return nil
		},
		Now: func() time.Time { return now },
	}

	prepared := provider.PreFetch(context.Background())
	if liveCalls.Load() != 0 {
		t.Fatalf("PreFetch() queried live %d times, want 0 — it must not touch sources", liveCalls.Load())
	}
	got := prepared.Fetch(context.Background())

	if got.Origin != snapshot.OriginLive || got.Windows[0].UsedPercent != 43 {
		t.Fatalf("Prepared.Fetch() = origin/usage %q/%v, want live/43", got.Origin, got.Windows[0].UsedPercent)
	}
	if refreshCalls.Load() != 1 || liveCalls.Load() != 1 {
		t.Fatalf("refresh/live calls = %d/%d, want 1/1", refreshCalls.Load(), liveCalls.Load())
	}
}

func TestPreFetchSkipsTheCacheAndRefreshWhenLiveIsDisabled(t *testing.T) {
	now := time.Date(2026, 8, 29, 20, 0, 0, 0, time.UTC)
	store := cache.Store{Dir: t.TempDir()}
	if err := store.Save(providerWithWindow(snapshot.OriginLive, now.Add(time.Hour), 18)); err != nil {
		t.Fatal(err)
	}
	var refreshCalls atomic.Int32

	got := (hybrid.Provider{
		ID: "test", DisplayName: "Test",
		Local:          localSource(providerWithWindow(snapshot.OriginLocal, now.Add(time.Hour), 18)),
		Live:           liveSource(providerWithWindow(snapshot.OriginLive, now.Add(time.Hour), 43)),
		LiveEnabled:    false,
		Cache:          &store,
		ShortestWindow: 5 * time.Hour,
		TokenStale:     func(time.Time) bool { return true },
		Refresh: func(context.Context) error {
			refreshCalls.Add(1)
			return nil
		},
		Now: func() time.Time { return now },
	}).Fetch(context.Background())

	if refreshCalls.Load() != 0 || got.Origin != snapshot.OriginLocal || got.Windows[0].UsedPercent != 18 {
		t.Fatalf("refresh calls = %d, Fetch() = %#v, want no refresh and the local reading", refreshCalls.Load(), got)
	}
}

func providerWithWindow(origin snapshot.Origin, resetsAt time.Time, percent float64) snapshot.Provider {
	return snapshot.Provider{
		ID: "test", DisplayName: "Test",
		Windows:    []snapshot.Window{{ID: "window", Label: "5h", Kind: snapshot.KindSession, UsedPercent: percent, ResetsAt: &snapshot.Time{Time: resetsAt}}},
		ObservedAt: snapshot.Time{Time: resetsAt.Add(-time.Hour)},
		Origin:     origin, Status: snapshot.OK(),
	}
}

func providerWithCredits(origin snapshot.Origin, observedAt time.Time) snapshot.Provider {
	balance := "$7.75 this month"
	return snapshot.Provider{
		ID: "test", DisplayName: "Test",
		Credits:    &snapshot.Credits{Unlimited: true, Balance: &balance, Enabled: true},
		ObservedAt: snapshot.Time{Time: observedAt},
		Origin:     origin, Status: snapshot.OK(),
	}
}

func localSource(provider snapshot.Provider) stubSource {
	return stubSource{origin: snapshot.OriginLocal, provider: provider}
}

func liveSource(provider snapshot.Provider) stubSource {
	return stubSource{origin: snapshot.OriginLive, provider: provider}
}
