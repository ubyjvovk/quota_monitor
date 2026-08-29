// Package grok reads Grok subscription quota from its live billing source.
package grok

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"quotamon/internal/jsonx"
	"quotamon/internal/snapshot"
	"quotamon/internal/source"
)

const (
	// ProviderID is Grok's stable provider identifier.
	ProviderID = "grok"
	// DisplayName is Grok's human-readable provider name.
	DisplayName = "Grok"
)

const credentialScopePrefix = "https://auth.x.ai::"

// DefaultAuthPath returns the Grok CLI credential file path.
func DefaultAuthPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".grok", "auth.json")
}

// Credentials contains the Grok OAuth fields needed by the live source.
type Credentials struct {
	// Token is the Grok CLI bearer token.
	Token string
	// ExpiresAt is the provider-reported token expiry hint.
	ExpiresAt *time.Time
}

// ParseCredentials extracts the first deterministically ordered xAI OAuth scope.
// The blob also contains profile data and may gain unrelated credential-shaped
// fields, so this parser addresses only the selected scope and its required keys.
func ParseCredentials(blob []byte) (Credentials, error) {
	root, err := jsonx.Parse(blob)
	if err != nil {
		return Credentials{}, source.Errorf(source.Malformed, "Unrecognised Grok credentials — run `grok login` again")
	}
	object, ok := root.(map[string]any)
	if !ok {
		return Credentials{}, noTokenError()
	}

	keys := make([]string, 0, len(object))
	for key := range object {
		if strings.HasPrefix(key, credentialScopePrefix) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		return Credentials{}, noTokenError()
	}

	tokenValue, _ := jsonx.Get(root, keys[0], "key")
	token, _ := jsonx.String(tokenValue)
	if token == "" {
		return Credentials{}, noTokenError()
	}

	credentials := Credentials{Token: token}
	if value, found := jsonx.Get(root, keys[0], "expires_at"); found {
		if expiresAt, valid := jsonx.Time(value); valid {
			credentials.ExpiresAt = &expiresAt
		}
	}
	return credentials, nil
}

// Snapshot normalises a Grok billing response into its single shared-pool window.
func Snapshot(root any, observedAt time.Time) (snapshot.Provider, bool) {
	config, ok := jsonx.Get(root, "config")
	if !ok {
		return snapshot.Provider{}, false
	}
	usedValue, ok := jsonx.Get(config, "creditUsagePercent")
	if !ok {
		return snapshot.Provider{}, false
	}
	usedPercent, ok := jsonx.Float(usedValue)
	if !ok {
		return snapshot.Provider{}, false
	}

	var resetsAt *snapshot.Time
	var periodEnd time.Time
	if value, found := jsonx.Get(config, "currentPeriod", "end"); found {
		if parsed, valid := jsonx.Time(value); valid {
			periodEnd = parsed
			resetsAt = &snapshot.Time{Time: parsed}
		}
	}

	var windowMinutes *int
	var periodStart time.Time
	if value, found := jsonx.Get(config, "currentPeriod", "start"); found {
		periodStart, _ = jsonx.Time(value)
	}
	if !periodStart.IsZero() && !periodEnd.IsZero() {
		minutes := int(periodEnd.Sub(periodStart).Minutes())
		windowMinutes = &minutes
	} else if value, found := jsonx.Get(config, "currentPeriod", "type"); found {
		if periodType, valid := jsonx.String(value); valid && periodType == "USAGE_PERIOD_TYPE_WEEKLY" {
			minutes := 7 * 24 * 60
			windowMinutes = &minutes
		}
	}

	var credits *snapshot.Credits
	if value, found := jsonx.Get(config, "prepaidBalance", "val"); found {
		if prepaidBalance, valid := jsonx.Float(value); valid && prepaidBalance > 0 {
			balance := fmt.Sprintf("%.2f", prepaidBalance)
			credits = &snapshot.Credits{
				HasCredits: true,
				Unlimited:  false,
				Balance:    &balance,
				Enabled:    true,
			}
		}
	}

	return snapshot.Provider{
		ID:          ProviderID,
		DisplayName: DisplayName,
		Windows: []snapshot.Window{{
			ID:            "credits",
			Label:         "Week",
			Kind:          snapshot.KindWeekly,
			UsedPercent:   usedPercent,
			ResetsAt:      resetsAt,
			WindowMinutes: windowMinutes,
		}},
		Credits:    credits,
		ObservedAt: snapshot.Time{Time: observedAt},
		Origin:     snapshot.OriginLive,
		Status:     snapshot.OK(),
	}, true
}

func noTokenError() error {
	return source.Errorf(source.NotConfigured, "Run `grok login` — no Grok token in %s", DefaultAuthPath())
}
