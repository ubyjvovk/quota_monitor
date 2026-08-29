// Package kimi reads Kimi (Moonshot) coding quota from its live usages endpoint.
//
// The endpoint path is /usages (plural); the singular /usage and a dozen other
// guessed routes are 404, and the /me route returns identity data including a
// phone number that must never be ingested. The bearer token lives only 15
// minutes; stale-token caching and any launch of the Kimi TUI are kept outside
// the live source so this package never rotates the CLI-owned refresh token.
package kimi

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"quotamon/internal/jsonx"
	"quotamon/internal/snapshot"
	"quotamon/internal/source"
)

const (
	// ProviderID is Kimi's stable provider identifier.
	ProviderID = "kimi"
	// DisplayName is Kimi's human-readable provider name.
	DisplayName = "Kimi"
)

// weeklyWindowMinutes is the weekly usage pool's rollover length. The body does
// not carry a duration for this pool; the TUI always labels it a week.
const weeklyWindowMinutes = 10080

// DefaultCredentialPath returns the Kimi CLI credential file path.
func DefaultCredentialPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".kimi-code", "credentials", "kimi-code.json")
}

// Credentials contains the Kimi OAuth fields needed by the live source.
type Credentials struct {
	// Token is the Kimi CLI bearer token.
	Token string
	// ExpiresAt is the provider-reported token expiry hint.
	ExpiresAt *time.Time
}

// ParseCredentials extracts the Kimi access token. The blob also carries a
// refresh token, scope, and profile data, so this parser addresses only the
// top-level access_token and expires_at keys by explicit path — never a
// recursive search that could surface an unrelated credential later in the blob.
func ParseCredentials(blob []byte) (Credentials, error) {
	root, err := jsonx.Parse(blob)
	if err != nil {
		return Credentials{}, source.Errorf(source.Malformed, "Unrecognised Kimi credentials — run `kimi` and sign in again")
	}
	tokenValue, ok := jsonx.Get(root, "access_token")
	if !ok {
		return Credentials{}, noTokenError()
	}
	token, valid := jsonx.String(tokenValue)
	if !valid || token == "" {
		return Credentials{}, noTokenError()
	}

	credentials := Credentials{Token: token}
	if value, found := jsonx.Get(root, "expires_at"); found {
		if expiresAt, ok := jsonx.Time(value); ok {
			credentials.ExpiresAt = &expiresAt
		}
	}
	return credentials, nil
}

func noTokenError() error {
	return source.Errorf(source.NotConfigured, "Run `kimi` and sign in — no Kimi token in %s", DefaultCredentialPath())
}

// Snapshot normalises a Kimi usages response into its weekly pool plus one
// window per extra limit. The weekly pool carries no duration in the body, so
// it is fixed at seven days; the extra windows describe their own durations in
// minutes. Kimi reports usage numbers as strings, so the percentages are
// computed by parsing those strings and never by assuming a number type.
// ok is false when the body yields no usable window.
func Snapshot(root any, observedAt time.Time) (snapshot.Provider, bool) {
	var plan *string
	if value, found := jsonx.Get(root, "user", "membership", "level"); found {
		if level, valid := jsonx.String(value); valid {
			normalised := strings.ToLower(strings.TrimPrefix(level, "LEVEL_"))
			plan = &normalised
		}
	}

	windows := make([]snapshot.Window, 0, 1)
	if window, ok := weeklyWindow(root); ok {
		windows = append(windows, window)
	}
	windows = append(windows, limitWindows(root)...)
	if len(windows) == 0 {
		return snapshot.Provider{}, false
	}

	return snapshot.Provider{
		ID:          ProviderID,
		DisplayName: DisplayName,
		Plan:        plan,
		Windows:     windows,
		Credits:     nil,
		ObservedAt:  snapshot.Time{Time: observedAt},
		Origin:      snapshot.OriginLive,
		Status:      snapshot.OK(),
	}, true
}

// weeklyWindow reads the top-level usage pool. The weekly pool has no explicit
// duration, so the window length is fixed; a missing, unparsable, or zero limit
// yields no weekly window rather than a fabricated reading.
func weeklyWindow(root any) (snapshot.Window, bool) {
	usedValue, ok := jsonx.Get(root, "usage", "used")
	if !ok {
		return snapshot.Window{}, false
	}
	limitValue, ok := jsonx.Get(root, "usage", "limit")
	if !ok {
		return snapshot.Window{}, false
	}
	usedPercent, ok := usagePercent(usedValue, limitValue)
	if !ok {
		return snapshot.Window{}, false
	}

	var resetsAt *snapshot.Time
	if value, found := jsonx.Get(root, "usage", "resetTime"); found {
		if reset, valid := jsonx.Time(value); valid {
			resetsAt = &snapshot.Time{Time: reset}
		}
	}
	minutes := weeklyWindowMinutes
	return snapshot.Window{
		ID:            "weekly",
		Label:         "Week",
		Kind:          snapshot.KindWeekly,
		UsedPercent:   usedPercent,
		ResetsAt:      resetsAt,
		WindowMinutes: &minutes,
	}, true
}

// limitWindows turns each entry of the extra limits array into a window.
// Entries whose duration/time unit, used, or limit cannot be understood are
// skipped individually rather than dropping the whole reading.
func limitWindows(root any) []snapshot.Window {
	value, found := jsonx.Get(root, "limits")
	if !found {
		return nil
	}
	entries, ok := value.([]any)
	if !ok {
		return nil
	}
	windows := make([]snapshot.Window, 0, len(entries))
	for _, entry := range entries {
		if window, ok := limitWindow(entry); ok {
			windows = append(windows, window)
		}
	}
	return windows
}

// limitWindow normalises one extra usage window from its window.duration,
// window.timeUnit, and detail.used/limit fields.
func limitWindow(entry any) (snapshot.Window, bool) {
	durationValue, ok := jsonx.Get(entry, "window", "duration")
	if !ok {
		return snapshot.Window{}, false
	}
	duration, ok := jsonx.Int(durationValue)
	if !ok {
		return snapshot.Window{}, false
	}
	unitMinutes, ok := timeUnitMinutes(entry)
	if !ok {
		return snapshot.Window{}, false
	}
	minutes := duration * unitMinutes

	usedValue, ok := jsonx.Get(entry, "detail", "used")
	if !ok {
		return snapshot.Window{}, false
	}
	limitValue, ok := jsonx.Get(entry, "detail", "limit")
	if !ok {
		return snapshot.Window{}, false
	}
	usedPercent, ok := usagePercent(usedValue, limitValue)
	if !ok {
		return snapshot.Window{}, false
	}

	var resetsAt *snapshot.Time
	if value, found := jsonx.Get(entry, "detail", "resetTime"); found {
		if reset, valid := jsonx.Time(value); valid {
			resetsAt = &snapshot.Time{Time: reset}
		}
	}

	return snapshot.Window{
		ID:            fmt.Sprintf("limit_%dm", minutes),
		Label:         snapshot.LabelFromMinutes(&minutes),
		Kind:          snapshot.KindFromMinutes(&minutes),
		UsedPercent:   usedPercent,
		ResetsAt:      resetsAt,
		WindowMinutes: &minutes,
	}, true
}

// timeUnitMinutes returns how many minutes one unit of the window's duration
// represents. An unknown time unit means the duration cannot be trusted, so it
// is skipped rather than guessed.
func timeUnitMinutes(entry any) (int, bool) {
	value, ok := jsonx.Get(entry, "window", "timeUnit")
	if !ok {
		return 0, false
	}
	unit, ok := jsonx.String(value)
	if !ok {
		return 0, false
	}
	switch unit {
	case "TIME_UNIT_MINUTE":
		return 1, true
	case "TIME_UNIT_HOUR":
		return 60, true
	case "TIME_UNIT_DAY":
		return 1440, true
	default:
		return 0, false
	}
}

// usagePercent parses a used/limit pair, which Kimi reports as strings, and
// returns used/limit*100. A limit that is missing, unparsable, or zero yields
// no percentage — we must never divide by zero or invent a ceiling that is
// absent.
func usagePercent(usedValue, limitValue any) (float64, bool) {
	used, ok := parseNumber(usedValue)
	if !ok {
		return 0, false
	}
	limit, ok := parseNumber(limitValue)
	if !ok || limit == 0 {
		return 0, false
	}
	return used / limit * 100, true
}

// parseNumber reads a Kimi usage number, which is always serialised as a string.
func parseNumber(value any) (float64, bool) {
	text, ok := jsonx.String(value)
	if !ok {
		return 0, false
	}
	parsed, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return 0, false
	}
	return parsed, true
}
