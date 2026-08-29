// Package discover probes local credentials without contacting provider APIs.
package discover

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"quotamon/internal/config"
)

// Finding describes whether one provider's local credentials are available.
type Finding struct {
	ID          string
	DisplayName string
	Found       bool
	Supported   bool
	Detail      string
	Hint        string
	NeedsKey    bool
}

type dependencies struct {
	goos       string
	home       string
	getenv     func(string) string
	run        func(string, ...string) error
	loadConfig func() (config.Config, error)
	now        func() time.Time
}

// All probes every known provider using local files, environment variables,
// and (on macOS) the exit status of the security CLI, in stable display order.
func All() []Finding {
	home, _ := os.UserHomeDir()
	return all(dependencies{
		goos:   runtime.GOOS,
		home:   home,
		getenv: os.Getenv,
		run: func(name string, arguments ...string) error {
			return exec.Command(name, arguments...).Run()
		},
		loadConfig: config.Load,
		now:        time.Now,
	})
}

func all(deps dependencies) []Finding {
	return []Finding{
		discoverClaude(deps),
		discoverFile("codex", "ChatGPT", filepath.Join(deps.home, ".codex", "auth.json"), "~/.codex/auth.json", "run `codex login`", true),
		discoverGrok(deps),
		discoverDeepInfra(deps),
		discoverFile("kimi", "Kimi", filepath.Join(deps.home, ".kimi-code", "credentials", "kimi-code.json"), "~/.kimi-code/credentials/kimi-code.json", "no quota API found — see PROVIDERS.md", false),
	}
}

func discoverClaude(deps dependencies) Finding {
	finding := Finding{ID: "claude", DisplayName: "Claude", Supported: true, Hint: "run `claude` and sign in"}
	if deps.goos == "darwin" {
		finding.Found = deps.run("security", "find-generic-password", "-s", "Claude Code-credentials") == nil
		finding.Detail = "Keychain item not found"
		if finding.Found {
			finding.Detail = "Keychain item present"
		}
		return finding
	}
	return discoverFile("claude", "Claude", filepath.Join(deps.home, ".claude", ".credentials.json"), "~/.claude/.credentials.json", finding.Hint, true)
}

func discoverFile(id, displayName, path, displayPath, hint string, supported bool) Finding {
	_, err := os.Stat(path)
	found := err == nil
	detail := displayPath + " not found"
	if found {
		detail = displayPath
	}
	return Finding{ID: id, DisplayName: displayName, Found: found, Supported: supported, Detail: detail, Hint: hint}
}

func discoverGrok(deps dependencies) Finding {
	path := filepath.Join(deps.home, ".grok", "auth.json")
	finding := Finding{ID: "grok", DisplayName: "Grok", Supported: true, Detail: "~/.grok/auth.json not found", Hint: "run `grok login`"}
	data, err := os.ReadFile(path)
	if err != nil {
		return finding
	}
	var root map[string]json.RawMessage
	if json.Unmarshal(data, &root) != nil {
		return finding
	}
	for key, value := range root {
		if !strings.HasPrefix(key, "https://auth.x.ai::") {
			continue
		}
		finding.Found = true
		finding.Detail = "~/.grok/auth.json"
		var credential struct {
			ExpiresAt json.RawMessage `json:"expires_at"`
		}
		if json.Unmarshal(value, &credential) == nil {
			if expiresAt, ok := parseExpiry(credential.ExpiresAt); ok {
				finding.Detail += " (" + expiryDetail(expiresAt, deps.now()) + ")"
			}
		}
		return finding
	}
	return finding
}

func discoverDeepInfra(deps dependencies) Finding {
	finding := Finding{
		ID: "deepinfra", DisplayName: "DeepInfra", Supported: true, NeedsKey: true,
		Detail: "DEEPINFRA_KEY not set", Hint: "get a key at deepinfra.com/dash/api_keys",
	}
	if deps.getenv("DEEPINFRA_KEY") != "" {
		finding.Found = true
		finding.Detail = "DEEPINFRA_KEY set"
		return finding
	}
	loaded, err := deps.loadConfig()
	if err != nil && !errors.Is(err, config.ErrMissing) {
		return finding
	}
	if loaded.Providers["deepinfra"].APIKey != "" {
		finding.Found = true
		finding.Detail = "config.json api_key set"
	}
	return finding
}

func parseExpiry(raw json.RawMessage) (time.Time, bool) {
	if len(raw) == 0 {
		return time.Time{}, false
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		if parsed, err := time.Parse(time.RFC3339, text); err == nil {
			return parsed, true
		}
		value, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return time.Time{}, false
		}
		return numericTime(value), true
	}
	var value float64
	if json.Unmarshal(raw, &value) != nil {
		return time.Time{}, false
	}
	return numericTime(value), true
}

func numericTime(value float64) time.Time {
	if value > 10_000_000_000 {
		value /= 1000
	}
	seconds := int64(value)
	nanoseconds := int64((value - float64(seconds)) * float64(time.Second))
	return time.Unix(seconds, nanoseconds)
}

func expiryDetail(expiresAt, now time.Time) string {
	remaining := expiresAt.Sub(now)
	if remaining < 0 {
		return fmt.Sprintf("token expired %s ago", shortDuration(-remaining))
	}
	return fmt.Sprintf("token expires in %s", shortDuration(remaining))
}

func shortDuration(duration time.Duration) string {
	if duration >= time.Hour {
		hours := int(duration.Round(time.Hour) / time.Hour)
		if hours < 1 {
			hours = 1
		}
		return fmt.Sprintf("%dh", hours)
	}
	minutes := int(duration.Round(time.Minute) / time.Minute)
	if minutes < 1 {
		minutes = 1
	}
	return fmt.Sprintf("%dm", minutes)
}
