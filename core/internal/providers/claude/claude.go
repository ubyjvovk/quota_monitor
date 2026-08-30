// Package claude reads Claude subscription quota from local and live sources.
package claude

import (
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"quotamon/internal/jsonx"
	"quotamon/internal/snapshot"
	"quotamon/internal/source"
)

const (
	// ProviderID is Claude's stable provider identifier.
	ProviderID = "claude"
	// DisplayName is Claude's human-readable provider name.
	DisplayName = "Claude"
	// KeychainService is the service name of Claude Code's macOS credential item.
	KeychainService = "Claude Code-credentials"
)

var (
	runSecurity = func() ([]byte, error) {
		return exec.Command("/usr/bin/security", "find-generic-password", "-s", KeychainService, "-w").Output()
	}
	readCredentialFile = os.ReadFile
)

// DefaultMirrorPath returns the statusline mirror path, honoring QUOTA_MONITOR_DIR.
func DefaultMirrorPath() string {
	if directory := os.Getenv("QUOTA_MONITOR_DIR"); directory != "" {
		return filepath.Join(directory, "claude-usage.json")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".quota-monitor", "claude-usage.json")
}

// ReadCredentials returns Claude Code's raw credential JSON for the current OS.
func ReadCredentials() ([]byte, error) {
	if runtime.GOOS == "darwin" {
		blob, err := runSecurity()
		if err == nil {
			return blob, nil
		}

		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			switch exitError.ExitCode() {
			case 44:
				return nil, source.Errorf(source.NotConfigured, "No Claude credentials in the Keychain — run `claude` and sign in")
			case 51, 128:
				return nil, source.Errorf(source.Unauthorized, "Keychain access denied — run `claude` and approve access")
			}
		}
		return nil, source.Errorf(source.Transport, "Could not read Claude credentials from the Keychain: %v", err)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return nil, source.Errorf(source.Transport, "Could not locate Claude credentials: %v", err)
	}
	path := filepath.Join(home, ".claude", ".credentials.json")
	blob, err := readCredentialFile(path)
	if err == nil {
		return blob, nil
	}
	if os.IsNotExist(err) {
		return nil, source.Errorf(source.NotConfigured, "No Claude credentials at ~/.claude/.credentials.json — run `claude` and sign in")
	}
	return nil, source.Errorf(source.Transport, "Could not read Claude credentials at ~/.claude/.credentials.json: %v", err)
}

// Credentials contains the Claude OAuth fields needed by the live source.
type Credentials struct {
	// Token is Claude Code's OAuth access token.
	Token string
	// ExpiresAt is the provider-reported token expiry hint.
	ExpiresAt *time.Time
	// Plan is the provider-reported subscription type.
	Plan string
}

// ParseCredentials extracts only Claude's explicitly addressed OAuth subtree.
// The same blob stores MCP tokens under mcpOAuth, so recursive token searches
// can authenticate as the wrong service; this parser never searches loosely.
func ParseCredentials(blob []byte) (Credentials, error) {
	root, err := jsonx.Parse(blob)
	if err != nil {
		return Credentials{}, source.Errorf(source.Malformed, "Unrecognised Claude credentials — run `claude` and sign in again")
	}

	oauth := root
	if nested, ok := jsonx.Get(root, "claudeAiOauth"); ok {
		oauth = nested
	}
	tokenValue, _ := jsonx.Get(oauth, "accessToken")
	token, _ := jsonx.String(tokenValue)
	if token == "" {
		return Credentials{}, source.Errorf(source.NotConfigured, "No Claude access token — run `claude` and sign in")
	}

	credentials := Credentials{Token: token}
	if value, ok := jsonx.Get(oauth, "expiresAt"); ok {
		if expiresAt, valid := jsonx.Time(value); valid {
			credentials.ExpiresAt = &expiresAt
		}
	}
	if value, ok := jsonx.Get(oauth, "subscriptionType"); ok {
		credentials.Plan, _ = jsonx.String(value)
	}
	return credentials, nil
}

// Windows extracts canonical Claude limits, falling back to legacy mirror fields.
// The limits array is preferred because the endpoint's top-level fields omit
// scoped weekly limits, which can be the account's most constrained allowance.
func Windows(root any) []snapshot.Window {
	if value, ok := jsonx.Get(root, "limits"); ok {
		if entries, ok := value.([]any); ok {
			windows := make([]snapshot.Window, 0, len(entries))
			for _, entry := range entries {
				kindValue, kindOK := jsonx.Get(entry, "kind")
				groupValue, groupOK := jsonx.Get(entry, "group")
				percentValue, percentOK := jsonx.Get(entry, "percent")
				kind, kindOK := jsonx.String(kindValue)
				group, groupOK := jsonx.String(groupValue)
				percent, percentOK := jsonx.Float(percentValue)
				if !kindOK || !groupOK || !percentOK {
					continue
				}

				window := snapshot.Window{ID: kind, UsedPercent: percent}
				switch {
				case group == "session":
					window.Label = "5h"
					window.Kind = snapshot.KindSession
					window.WindowMinutes = intPointer(300)
				case kind == "weekly_all":
					window.Label = "Week"
					window.Kind = snapshot.KindWeekly
					window.WindowMinutes = intPointer(10080)
				case kind == "weekly_scoped":
					window.Label = "Week (scoped)"
					if nameValue, ok := jsonx.Get(entry, "scope", "model", "display_name"); ok {
						if name, ok := jsonx.String(nameValue); ok {
							window.Label = name + " wk"
						}
					}
					window.Kind = snapshot.KindWeekly
					window.WindowMinutes = intPointer(10080)
				default:
					continue
				}
				if resetValue, ok := jsonx.Get(entry, "resets_at"); ok {
					if resetsAt, valid := jsonx.Time(resetValue); valid {
						window.ResetsAt = &snapshot.Time{Time: resetsAt}
					}
				}
				windows = append(windows, window)
			}
			if len(windows) > 0 {
				return windows
			}
		}
	}

	specs := []struct {
		id            string
		label         string
		kind          snapshot.Kind
		windowMinutes int
	}{
		{id: "five_hour", label: "5h", kind: snapshot.KindSession, windowMinutes: 300},
		{id: "seven_day", label: "Week", kind: snapshot.KindWeekly, windowMinutes: 10080},
	}
	windows := make([]snapshot.Window, 0, len(specs))
	for _, spec := range specs {
		node, ok := jsonx.Get(root, spec.id)
		if !ok {
			continue
		}
		percentValue, ok := first(node, "used_percentage", "usedPercentage", "used_percent", "utilization")
		if !ok {
			continue
		}
		percent, ok := jsonx.Float(percentValue)
		if !ok {
			continue
		}
		window := snapshot.Window{
			ID:            spec.id,
			Label:         spec.label,
			Kind:          spec.kind,
			UsedPercent:   percent,
			WindowMinutes: intPointer(spec.windowMinutes),
		}
		if resetValue, ok := first(node, "resets_at", "resetsAt", "reset_at"); ok {
			if resetsAt, valid := jsonx.Time(resetValue); valid {
				window.ResetsAt = &snapshot.Time{Time: resetsAt}
			}
		}
		windows = append(windows, window)
	}
	return windows
}

// Snapshot builds a normalised Claude provider from a rate-limits object.
func Snapshot(root any, observedAt time.Time, origin snapshot.Origin, plan string) (snapshot.Provider, bool) {
	windows := Windows(root)
	if len(windows) == 0 {
		return snapshot.Provider{}, false
	}

	var credits *snapshot.Credits
	if spend, ok := jsonx.Get(root, "spend"); ok && spend != nil {
		enabled := true
		if value, ok := jsonx.Get(spend, "enabled"); ok {
			if parsed, valid := jsonx.Bool(value); valid {
				enabled = parsed
			}
		}

		balanceObject, _ := jsonx.Get(spend, "balance")
		balanceValue, balanceOK := money(balanceObject)
		var balance *string
		if balanceOK {
			balance = &balanceValue
		}

		usedObject, _ := jsonx.Get(spend, "used")
		used, usedOK := money(usedObject)
		limitObject, _ := jsonx.Get(spend, "limit")
		limit, limitOK := money(limitObject)
		var spendSummary *string
		switch {
		case usedOK && limitOK:
			value := used + " of " + limit + " this month"
			spendSummary = &value
		case usedOK:
			value := used + " this month"
			spendSummary = &value
		}

		balanceMinor, balanceAmountOK := getFloat(balanceObject, "amount_minor")
		credits = &snapshot.Credits{
			HasCredits: enabled && balanceOK && balanceAmountOK && balanceMinor > 0,
			Unlimited:  false,
			Balance:    balance,
			Enabled:    enabled,
			Spend:      spendSummary,
		}
	}

	if plan == "" {
		if value, ok := first(root, "subscription_type", "plan_type", "account_type"); ok {
			plan, _ = jsonx.String(value)
		}
	}
	var planPointer *string
	if plan != "" {
		planPointer = &plan
	}
	return snapshot.Provider{
		ID:          ProviderID,
		DisplayName: DisplayName,
		Plan:        planPointer,
		Windows:     windows,
		Credits:     credits,
		ObservedAt:  snapshot.Time{Time: observedAt},
		Origin:      origin,
		Status:      snapshot.OK(),
	}, true
}

func first(root any, paths ...string) (any, bool) {
	for _, path := range paths {
		if value, ok := jsonx.Get(root, path); ok {
			return value, true
		}
	}
	return nil, false
}

func getFloat(root any, path ...string) (float64, bool) {
	value, ok := jsonx.Get(root, path...)
	if !ok {
		return 0, false
	}
	return jsonx.Float(value)
}

func getInt(root any, path ...string) (int, bool) {
	value, ok := jsonx.Get(root, path...)
	if !ok {
		return 0, false
	}
	return jsonx.Int(value)
}

func money(object any) (string, bool) {
	amountMinor, ok := getFloat(object, "amount_minor")
	if !ok {
		return "", false
	}

	exponent := 2
	if parsed, ok := getInt(object, "exponent"); ok {
		exponent = parsed
	}
	if exponent < 0 {
		return "", false
	}

	currency := "USD"
	if value, ok := jsonx.Get(object, "currency"); ok {
		if parsed, valid := jsonx.String(value); valid && parsed != "" {
			currency = parsed
		}
	}
	formatted := fmt.Sprintf("%.*f", exponent, amountMinor/math.Pow10(exponent))
	if currency == "USD" {
		return "$" + formatted, true
	}
	return formatted + " " + currency, true
}

func intPointer(value int) *int {
	return &value
}
