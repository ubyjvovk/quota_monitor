// Package codex reads ChatGPT quota through the Codex CLI, rollout files, or HTTP.
package codex

import (
	"os"
	"path/filepath"
	"time"

	"quotamon/internal/jsonx"
	"quotamon/internal/snapshot"
)

const (
	// ProviderID is the stable identifier used for ChatGPT snapshots.
	ProviderID = "codex"
	// DisplayName is the provider name shown to users.
	DisplayName = "ChatGPT"
)

// DefaultHome returns the default Codex CLI data directory.
func DefaultHome() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".codex")
}

// Window parses one Codex rate-limit window using explicit keys on that node.
func Window(node any, id string) (snapshot.Window, bool) {
	usedValue, ok := first(node, "used_percent", "usedPercent")
	if !ok {
		return snapshot.Window{}, false
	}
	used, ok := jsonx.Float(usedValue)
	if !ok {
		return snapshot.Window{}, false
	}

	var minutes *int
	if value, found := first(node, "window_minutes", "windowDurationMins", "windowMinutes"); found {
		if parsed, valid := jsonx.Int(value); valid {
			minutes = &parsed
		}
	}

	var resetsAt *snapshot.Time
	if value, found := first(node, "resets_at", "resetsAt"); found {
		if parsed, valid := jsonx.Time(value); valid {
			resetsAt = &snapshot.Time{Time: parsed}
		}
	}

	return snapshot.Window{
		ID:            id,
		Label:         snapshot.LabelFromMinutes(minutes),
		Kind:          snapshot.KindFromMinutes(minutes),
		UsedPercent:   used,
		ResetsAt:      resetsAt,
		WindowMinutes: minutes,
	}, true
}

// Snapshot normalises one Codex rate-limits object into a provider snapshot.
func Snapshot(limits any, observedAt time.Time, origin snapshot.Origin) (snapshot.Provider, bool) {
	windows := make([]snapshot.Window, 0, 2)
	for _, id := range []string{"primary", "secondary"} {
		node, ok := jsonx.Get(limits, id)
		if !ok {
			continue
		}
		if window, valid := Window(node, id); valid {
			windows = append(windows, window)
		}
	}
	if len(windows) == 0 {
		return snapshot.Provider{}, false
	}

	var credits *snapshot.Credits
	if node, ok := jsonx.Get(limits, "credits"); ok {
		hasCredits := false
		if value, found := first(node, "has_credits", "hasCredits"); found {
			hasCredits, _ = jsonx.Bool(value)
		}
		unlimited := false
		if value, found := jsonx.Get(node, "unlimited"); found {
			unlimited, _ = jsonx.Bool(value)
		}
		var balance *string
		if value, found := jsonx.Get(node, "balance"); found {
			if parsed, valid := jsonx.String(value); valid {
				balance = &parsed
			}
		}
		credits = &snapshot.Credits{
			HasCredits: hasCredits,
			Unlimited:  unlimited,
			Balance:    balance,
			Enabled:    hasCredits || unlimited,
		}
	}

	var plan *string
	if value, ok := first(limits, "plan_type", "planType"); ok {
		if parsed, valid := jsonx.String(value); valid {
			plan = &parsed
		}
	}

	return snapshot.Provider{
		ID:          ProviderID,
		DisplayName: DisplayName,
		Plan:        plan,
		Windows:     windows,
		Credits:     credits,
		ObservedAt:  snapshot.Time{Time: observedAt},
		Origin:      origin,
		Status:      snapshot.OK(),
	}, true
}

func first(node any, keys ...string) (any, bool) {
	for _, key := range keys {
		if value, ok := jsonx.Get(node, key); ok {
			return value, true
		}
	}
	return nil, false
}
