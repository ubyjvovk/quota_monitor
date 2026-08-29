package cache_test

import (
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"quotamon/internal/cache"
	"quotamon/internal/snapshot"
)

func TestStoreRoundTripsAProvider(t *testing.T) {
	store := cache.Store{Dir: t.TempDir()}
	want := cachedProvider(41)
	if err := store.Save(want); err != nil {
		t.Fatal(err)
	}

	got, ok := store.Load(want.ID)
	if !ok || !reflect.DeepEqual(got, want) {
		t.Fatalf("Load() = (%#v, %v), want (%#v, true)", got, ok, want)
	}
}

func TestStoreProtectsCacheFilesWithMode0600(t *testing.T) {
	directory := t.TempDir()
	store := cache.Store{Dir: directory}
	if err := store.Save(cachedProvider(41)); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(filepath.Join(directory, "test.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("cache mode = %#o, want 0600", got)
	}
}

func TestStoreKeepsThePreviousEntryWhenAReplacementCannotBeEncoded(t *testing.T) {
	directory := t.TempDir()
	store := cache.Store{Dir: directory}
	want := cachedProvider(41)
	if err := store.Save(want); err != nil {
		t.Fatal(err)
	}

	invalid := cachedProvider(math.NaN())
	if err := store.Save(invalid); err == nil {
		t.Fatal("Save() with NaN succeeded, want an encoding error")
	}
	got, ok := store.Load(want.ID)
	if !ok || !reflect.DeepEqual(got, want) {
		t.Fatalf("Load() after failed replacement = (%#v, %v), want previous entry", got, ok)
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "test.json" {
		t.Fatalf("cache directory after failed replacement = %#v", entries)
	}
}

func TestStoreAtomicallyReplacesTheCacheFile(t *testing.T) {
	directory := t.TempDir()
	store := cache.Store{Dir: directory}
	path := filepath.Join(directory, "test.json")
	if err := store.Save(cachedProvider(41)); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	if err := store.Save(cachedProvider(42)); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if os.SameFile(before, after) {
		t.Fatal("Save() rewrote the cache inode in place, want temp-file replacement")
	}
	provider, ok := store.Load("test")
	if !ok || provider.Windows[0].UsedPercent != 42 {
		t.Fatalf("Load() after replacement = (%#v, %v), want usage 42", provider, ok)
	}
}

func cachedProvider(percent float64) snapshot.Provider {
	observedAt := time.Date(2026, 8, 29, 20, 0, 0, 0, time.UTC)
	resetsAt := snapshot.Time{Time: observedAt.Add(5 * time.Hour)}
	return snapshot.Provider{
		ID: "test", DisplayName: "Test",
		Windows:    []snapshot.Window{{ID: "session", Label: "5h", Kind: snapshot.KindSession, UsedPercent: percent, ResetsAt: &resetsAt}},
		ObservedAt: snapshot.Time{Time: observedAt},
		Origin:     snapshot.OriginLive,
		Status:     snapshot.OK(),
	}
}
