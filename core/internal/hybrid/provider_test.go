package hybrid_test

import (
	"context"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

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
