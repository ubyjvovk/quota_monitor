// Package snapshot defines QuotaMon's cross-language snapshot contract.
package snapshot

import (
	"encoding/json"
	"strconv"
	"time"
)

// Kind identifies the duration class of a quota window.
type Kind string

const (
	// KindSession is a short rolling window.
	KindSession Kind = "session"
	// KindWeekly is a window shorter than ten days and at least one day long.
	KindWeekly Kind = "weekly"
	// KindMonthly is a window at least ten days long.
	KindMonthly Kind = "monthly"
	// KindOther is used when a window duration is unavailable.
	KindOther Kind = "other"
)

// Origin identifies where a provider reading came from.
type Origin string

const (
	// OriginLive identifies a reading fetched from a provider endpoint.
	OriginLive Origin = "live"
	// OriginLocal identifies a reading cached by a local CLI.
	OriginLocal Origin = "local"
	// OriginUnavailable identifies a provider without a usable reading.
	OriginUnavailable Origin = "unavailable"
)

// Time wraps time.Time with the snapshot contract's JSON representation.
type Time struct {
	// Time is the wrapped standard-library timestamp.
	time.Time
}

// MarshalJSON emits RFC 3339 UTC without fractional seconds.
func (t Time) MarshalJSON() ([]byte, error) {
	value := t.UTC().Truncate(time.Second).Format(time.RFC3339)
	return json.Marshal(value)
}

// UnmarshalJSON accepts RFC 3339 timestamps with offsets and fractional seconds.
func (t *Time) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return err
	}
	t.Time = parsed
	return nil
}

// Window is one provider rate-limit window normalised for display.
type Window struct {
	// ID is stable within its provider.
	ID string `json:"id"`
	// Label is the compact human-readable duration.
	Label string `json:"label"`
	// Kind is the duration class used for ordering.
	Kind Kind `json:"kind"`
	// UsedPercent is the provider-reported usage and may exceed 100.
	UsedPercent float64 `json:"usedPercent"`
	// ResetsAt is when the recorded usage ceases to describe the current window.
	ResetsAt *Time `json:"resetsAt,omitempty"`
	// WindowMinutes is the provider-reported window length.
	WindowMinutes *int `json:"windowMinutes,omitempty"`
}

// CurrentUsedPercent returns the reading only while its recorded window is current.
func (w Window) CurrentUsedPercent(now time.Time) (float64, bool) {
	if w.ResetsAt != nil && !w.ResetsAt.After(now) {
		return 0, false
	}
	return w.UsedPercent, true
}

// KindFromMinutes infers a duration class using QuotaKit's boundaries.
func KindFromMinutes(minutes *int) Kind {
	if minutes == nil {
		return KindOther
	}
	if *minutes < 60*24 {
		return KindSession
	}
	if *minutes < 60*24*10 {
		return KindWeekly
	}
	return KindMonthly
}

// LabelFromMinutes returns QuotaKit's compact label for a duration.
func LabelFromMinutes(minutes *int) string {
	if minutes == nil {
		return "Usage"
	}
	if *minutes%(60*24) == 0 {
		days := *minutes / (60 * 24)
		if days == 7 {
			return "Week"
		}
		return formatInt(days) + "d"
	}
	if *minutes%60 == 0 {
		return formatInt(*minutes/60) + "h"
	}
	return formatInt(*minutes) + "m"
}

func formatInt(value int) string {
	return strconv.Itoa(value)
}

// Credits is a provider-reported credit balance and whether it can be spent.
type Credits struct {
	// HasCredits reports whether the provider considers credits available.
	HasCredits bool `json:"hasCredits"`
	// Unlimited reports whether the provider considers the balance unlimited.
	Unlimited bool `json:"unlimited"`
	// Balance is the provider-formatted balance, when supplied.
	Balance *string `json:"balance,omitempty"`
	// Enabled reports whether spending the balance is currently enabled.
	Enabled bool `json:"enabled"`
}

// Status is the v2 cross-language provider status object.
type Status struct {
	// State is one of ok, needsSetup, or failed.
	State string `json:"state"`
	// Message explains a non-ok state in actionable terms.
	Message string `json:"message,omitempty"`
}

// OK returns a successful provider status.
func OK() Status {
	return Status{State: "ok"}
}

// NeedsSetup returns a status describing user action needed for a reading.
func NeedsSetup(message string) Status {
	return Status{State: "needsSetup", Message: message}
}

// Failed returns a status describing a provider attempt that failed.
func Failed(message string) Status {
	return Status{State: "failed", Message: message}
}

// Provider is one provider's normalised quota picture at a point in time.
type Provider struct {
	// ID is the stable provider identifier.
	ID string `json:"id"`
	// DisplayName is the provider name shown to users.
	DisplayName string `json:"displayName"`
	// Plan is the provider-reported subscription tier, when known.
	Plan *string `json:"plan,omitempty"`
	// Windows contains every quota window and always encodes as an array.
	Windows []Window `json:"windows"`
	// Credits is the provider's credit balance, when reported.
	Credits *Credits `json:"credits,omitempty"`
	// ObservedAt is when the quota numbers were true.
	ObservedAt Time `json:"observedAt"`
	// Origin identifies how the reading was obtained.
	Origin Origin `json:"origin"`
	// Status describes whether the provider is usable or needs attention.
	Status Status `json:"status"`
}

// MarshalJSON guarantees that Windows is an array even when the slice is nil.
func (p Provider) MarshalJSON() ([]byte, error) {
	type providerJSON Provider
	windows := p.Windows
	if windows == nil {
		windows = []Window{}
	}
	return json.Marshal(struct {
		providerJSON
		Windows []Window `json:"windows"`
	}{providerJSON: providerJSON(p), Windows: windows})
}

// Unavailable constructs a provider with no reading rather than a misleading zero.
func Unavailable(id, displayName string, status Status, observedAt time.Time) Provider {
	return Provider{
		ID:          id,
		DisplayName: displayName,
		Windows:     []Window{},
		ObservedAt:  Time{Time: observedAt},
		Origin:      OriginUnavailable,
		Status:      status,
	}
}

// TightestWindow returns the highest current usage, using kind rank to break ties.
func (p Provider) TightestWindow(now time.Time) (Window, bool) {
	var best Window
	bestPercent := 0.0
	found := false
	for _, window := range p.Windows {
		percent, current := window.CurrentUsedPercent(now)
		if !current {
			continue
		}
		if !found || percent > bestPercent || (percent == bestPercent && kindRank(window.Kind) < kindRank(best.Kind)) {
			best = window
			bestPercent = percent
			found = true
		}
	}
	return best, found
}

func kindRank(kind Kind) int {
	switch kind {
	case KindSession:
		return 0
	case KindWeekly:
		return 1
	case KindMonthly:
		return 2
	default:
		return 3
	}
}

// Snapshot is the complete normalised quota picture across providers.
type Snapshot struct {
	// Providers always encodes as an array.
	Providers []Provider `json:"providers"`
	// GeneratedAt is when the snapshot was assembled.
	GeneratedAt Time `json:"generatedAt"`
}

// MarshalJSON guarantees that Providers is an array even when the slice is nil.
func (s Snapshot) MarshalJSON() ([]byte, error) {
	type snapshotJSON Snapshot
	providers := s.Providers
	if providers == nil {
		providers = []Provider{}
	}
	return json.Marshal(struct {
		snapshotJSON
		Providers []Provider `json:"providers"`
	}{snapshotJSON: snapshotJSON(s), Providers: providers})
}

// Encode returns indented snapshot JSON using two-space indentation.
func (s Snapshot) Encode() ([]byte, error) {
	return json.MarshalIndent(s, "", "  ")
}

// Decode parses snapshot JSON and normalises nil provider and window slices.
func Decode(data []byte) (Snapshot, error) {
	var result Snapshot
	if err := json.Unmarshal(data, &result); err != nil {
		return Snapshot{}, err
	}
	if result.Providers == nil {
		result.Providers = []Provider{}
	}
	for index := range result.Providers {
		if result.Providers[index].Windows == nil {
			result.Providers[index].Windows = []Window{}
		}
	}
	return result, nil
}
