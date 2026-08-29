package snapshot_test

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"quotamon/internal/fixtures"
	"quotamon/internal/snapshot"
)

func TestSnapshotFixtureRoundTripsWithoutLosingJSONValues(t *testing.T) {
	fixture, err := fixtures.Load("snapshot-v2.json")
	if err != nil {
		t.Fatal(err)
	}

	decoded, err := snapshot.Decode(fixture)
	if err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	encoded, err := decoded.Encode()
	if err != nil {
		t.Fatalf("encode fixture: %v", err)
	}

	var want any
	if err := json.Unmarshal(fixture, &want); err != nil {
		t.Fatalf("parse fixture for comparison: %v", err)
	}
	var got any
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatalf("parse encoded snapshot for comparison: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip changed JSON\ngot:  %s\nwant: %s", encoded, fixture)
	}
}

func TestSnapshotAndProviderEncodeNilSlicesAsArrays(t *testing.T) {
	encoded, err := (snapshot.Snapshot{
		Providers: []snapshot.Provider{{
			ID:          "empty",
			DisplayName: "Empty",
			ObservedAt:  snapshot.Time{Time: time.Unix(0, 0)},
			Origin:      snapshot.OriginUnavailable,
			Status:      snapshot.NeedsSetup("configure it"),
		}},
		GeneratedAt: snapshot.Time{Time: time.Unix(0, 0)},
	}).Encode()
	if err != nil {
		t.Fatal(err)
	}

	var object map[string]any
	if err := json.Unmarshal(encoded, &object); err != nil {
		t.Fatal(err)
	}
	providers, ok := object["providers"].([]any)
	if !ok || len(providers) != 1 {
		t.Fatalf("providers = %#v, want one-element array", object["providers"])
	}
	provider := providers[0].(map[string]any)
	windows, ok := provider["windows"].([]any)
	if !ok || len(windows) != 0 {
		t.Fatalf("windows = %#v, want empty array", provider["windows"])
	}

	empty, err := (snapshot.Snapshot{GeneratedAt: snapshot.Time{Time: time.Unix(0, 0)}}).Encode()
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(empty, &object); err != nil {
		t.Fatal(err)
	}
	providers, ok = object["providers"].([]any)
	if !ok || len(providers) != 0 {
		t.Fatalf("providers = %#v, want empty array", object["providers"])
	}
}

func TestTimeEmitsWholeUTCSecondsAndAcceptsRFC3339Nano(t *testing.T) {
	input := time.Date(2026, 8, 29, 18, 59, 59, 741_925_000, time.UTC)
	encoded, err := json.Marshal(snapshot.Time{Time: input})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(encoded), `"2026-08-29T18:59:59Z"`; got != want {
		t.Fatalf("encoded time = %s, want %s", got, want)
	}

	tests := []struct {
		name  string
		input string
		want  time.Time
	}{
		{name: "whole UTC seconds decode", input: `"2026-08-29T18:59:59Z"`, want: input.Truncate(time.Second)},
		{name: "fractional seconds and offset decode", input: `"2026-08-29T18:59:59.741925+00:00"`, want: input},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var got snapshot.Time
			if err := json.Unmarshal([]byte(test.input), &got); err != nil {
				t.Fatal(err)
			}
			if !got.Equal(test.want) {
				t.Fatalf("decoded time = %s, want %s", got.Time, test.want)
			}
		})
	}
}

func TestCurrentUsedPercentRejectsRolledOverWindows(t *testing.T) {
	now := time.Date(2026, 8, 29, 19, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		reset   *snapshot.Time
		wantOK  bool
		wantUse float64
	}{
		{name: "missing reset stays current", wantOK: true, wantUse: 42},
		{name: "future reset stays current", reset: snapshotTime(now.Add(time.Minute)), wantOK: true, wantUse: 42},
		{name: "past reset has no reading", reset: snapshotTime(now.Add(-time.Minute)), wantOK: false},
		{name: "reset at now has no reading", reset: snapshotTime(now), wantOK: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := (snapshot.Window{UsedPercent: 42, ResetsAt: test.reset}).CurrentUsedPercent(now)
			if ok != test.wantOK || (ok && got != test.wantUse) {
				t.Fatalf("CurrentUsedPercent() = (%v, %v), want (%v, %v)", got, ok, test.wantUse, test.wantOK)
			}
		})
	}
}

func TestWindowKindAndLabelFollowQuotaKitDurationRules(t *testing.T) {
	tests := []struct {
		name      string
		minutes   *int
		wantKind  snapshot.Kind
		wantLabel string
	}{
		{name: "five hours is a session", minutes: intPointer(300), wantKind: snapshot.KindSession, wantLabel: "5h"},
		{name: "seven days is a week", minutes: intPointer(10080), wantKind: snapshot.KindWeekly, wantLabel: "Week"},
		{name: "two days is weekly with a day label", minutes: intPointer(2880), wantKind: snapshot.KindWeekly, wantLabel: "2d"},
		{name: "missing duration is generic usage", wantKind: snapshot.KindOther, wantLabel: "Usage"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := snapshot.KindFromMinutes(test.minutes); got != test.wantKind {
				t.Errorf("KindFromMinutes() = %q, want %q", got, test.wantKind)
			}
			if got := snapshot.LabelFromMinutes(test.minutes); got != test.wantLabel {
				t.Errorf("LabelFromMinutes() = %q, want %q", got, test.wantLabel)
			}
		})
	}
}

func TestTightestWindowIgnoresRolloverAndBreaksTiesByKind(t *testing.T) {
	now := time.Date(2026, 8, 29, 19, 0, 0, 0, time.UTC)
	provider := snapshot.Provider{Windows: []snapshot.Window{
		{ID: "rolled", Kind: snapshot.KindOther, UsedPercent: 99, ResetsAt: snapshotTime(now.Add(-time.Minute))},
		{ID: "weekly", Kind: snapshot.KindWeekly, UsedPercent: 50},
		{ID: "session", Kind: snapshot.KindSession, UsedPercent: 50},
	}}

	got, ok := provider.TightestWindow(now)
	if !ok || got.ID != "session" {
		t.Fatalf("TightestWindow() = (%q, %v), want session and true", got.ID, ok)
	}

	rolledOnly := snapshot.Provider{Windows: []snapshot.Window{
		{ID: "rolled", Kind: snapshot.KindSession, UsedPercent: 99, ResetsAt: snapshotTime(now)},
	}}
	if _, ok := rolledOnly.TightestWindow(now); ok {
		t.Fatal("TightestWindow() returned a rolled-over reading")
	}
}

func TestUnavailableCreatesAnEmptyNeedsAttentionProvider(t *testing.T) {
	observedAt := time.Date(2026, 8, 29, 19, 0, 0, 0, time.UTC)
	got := snapshot.Unavailable("kimi", "Kimi", snapshot.NeedsSetup("sign in"), observedAt)
	if got.ID != "kimi" || got.DisplayName != "Kimi" || got.Origin != snapshot.OriginUnavailable {
		t.Fatalf("Unavailable() = %#v", got)
	}
	if got.Windows == nil || len(got.Windows) != 0 {
		t.Fatalf("Unavailable().Windows = %#v, want non-nil empty slice", got.Windows)
	}
	if !got.ObservedAt.Equal(observedAt) {
		t.Fatalf("Unavailable().ObservedAt = %s, want %s", got.ObservedAt.Time, observedAt)
	}
}

func snapshotTime(value time.Time) *snapshot.Time {
	return &snapshot.Time{Time: value}
}

func intPointer(value int) *int {
	return &value
}
