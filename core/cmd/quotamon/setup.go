package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"

	"quotamon/internal/config"
	"quotamon/internal/discover"
)

// runSetup drives the first-run wizard: it discovers every provider, shows the
// findings, asks which to enable, and writes the mandatory config. Stdin and
// stdout are injected so the whole flow can be scripted in tests; the findings
// source is injected for the same reason (never touch the real Keychain).
//
// With yes set it writes without reading stdin at all: it enables every found
// supported provider (plus whatever a stale off entry would otherwise keep a
// newly supported, found provider pinned off), keeps existing choices and
// keys, and takes new keys from the environment only, never prompting.
func runSetup(stdin io.Reader, stdout, stderr io.Writer, yes bool, allFindings func() []discover.Finding) int {
	findings := allFindings()

	// Re-running setup starts from the on-disk state so pasted keys and manual
	// choices survive a re-run; an unreadable file (including ErrMissing) falls
	// back to fresh defaults rather than stranding the wizard.
	existing, loadErr := config.Load()
	newConfig := config.Default()
	if loadErr == nil {
		newConfig = existing
	}

	fmt.Fprintln(stdout, "Looking for providers…")
	fmt.Fprint(stdout, renderFindings(findings, nil))
	fmt.Fprintln(stdout)

	scanner := bufio.NewScanner(stdin)
	for _, finding := range findings {
		if !finding.Supported {
			continue
		}
		provider := newConfig.Providers[finding.ID]
		defaultEnabled := provider.Enabled
		if loadErr != nil {
			// First run (or a broken config): default Y when found, n otherwise.
			defaultEnabled = finding.Found
		}

		if yes {
			// Adopt every found supported provider even when an earlier config
			// wrote an explicit off entry for it (Kimi before it was supported),
			// while keeping a manually enabled provider with a stored key.
			provider.Enabled = finding.Found || provider.Enabled
		} else {
			enabled := resolveYesNo(ask(scanner, stdout, enablePrompt(finding.DisplayName, defaultEnabled)), defaultEnabled)
			provider.Enabled = enabled
			if enabled && finding.NeedsKey && provider.APIKey == "" {
				// A keyless DeepInfra would only fail at fetch time, so an empty
				// answer means "leave it off" rather than saving a broken provider.
				answer := ask(scanner, stdout, finding.DisplayName+" API key (input hidden is not available; paste and press Enter): ")
				if answer == "" {
					provider.Enabled = false
				} else {
					provider.APIKey = answer
				}
			}
		}
		newConfig.Providers[finding.ID] = provider
	}

	if yes {
		fmt.Fprintln(stdout, "Enabled every provider that was found; run without --yes to choose.")
	}

	if !yes {
		answer := ask(scanner, stdout, "Add anything else manually? [y/N] ")
		if strings.EqualFold(answer, "y") {
			addManually(scanner, stdout, &newConfig, supported(findings))
		}
	}

	if err := newConfig.Save(); err != nil {
		fmt.Fprintf(stderr, "setup: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "Wrote %s (mode 0600). Run: quotamon\n", config.Path())
	return 0
}

// runProviders lists every provider discovery sees alongside its on-disk
// enabled flag. It exists so a user can review what setup configured without
// re-running the wizard; it requires the config file because that is the whole
// point of the listing.
func runProviders(stdout, stderr io.Writer, allFindings func() []discover.Finding) int {
	cfg, err := config.Load()
	if err != nil {
		if errors.Is(err, config.ErrMissing) {
			fmt.Fprintln(stderr, "No config yet — run: quotamon setup")
			return 3
		}
		fmt.Fprintf(stderr, "config: %v\n", err)
		return 3
	}

	enabled := make(map[string]bool, len(cfg.Providers))
	for id, provider := range cfg.Providers {
		enabled[id] = provider.Enabled
	}
	fmt.Fprint(stdout, renderFindings(allFindings(), enabled))
	return 0
}

// renderFindings draws the provider table. enabled carries the on-disk flag for
// the providers command; a nil map omits that column (setup has no config to
// read yet).
func renderFindings(findings []discover.Finding, enabled map[string]bool) string {
	var builder strings.Builder
	for _, finding := range findings {
		line := fmt.Sprintf("%s %-9s", findingMark(finding), finding.DisplayName)
		if enabled != nil {
			line += fmt.Sprintf("  %-4s", findingEnabled(finding, enabled[finding.ID]))
		}
		line += "  " + findingDetail(finding) + "\n"
		builder.WriteString(line)
	}
	return builder.String()
}

func findingMark(finding discover.Finding) string {
	switch {
	case !finding.Supported:
		return "·"
	case finding.Found:
		return "✓"
	default:
		return "✗"
	}
}

func findingEnabled(finding discover.Finding, enabled bool) string {
	if !finding.Supported {
		return "n/a"
	}
	if enabled {
		return "on"
	}
	return "off"
}

// findingDetail turns a raw discovery finding into the copy a user reads in the
// wizard/listing. Found providers name their credential source; missing ones
// say how to fix it rather than dumping a raw path.
func findingDetail(finding discover.Finding) string {
	if !finding.Supported {
		if finding.Found {
			return "credentials found, but " + finding.DisplayName + " exposes no quota API yet"
		}
		return finding.Detail
	}
	if finding.Found {
		return finding.Detail
	}
	if finding.NeedsKey {
		return "no API key"
	}
	return "not signed in — " + finding.Hint
}

func enablePrompt(name string, defaultEnabled bool) string {
	if defaultEnabled {
		return fmt.Sprintf("Enable %s? [Y/n] ", name)
	}
	return fmt.Sprintf("Enable %s? [y/N] ", name)
}

// resolveYesNo maps a Y/n answer onto a bool; a blank or unrecognised line keeps
// the default rather than stealing focus with a second prompt.
func resolveYesNo(answer string, defaultEnabled bool) bool {
	switch strings.ToLower(answer) {
	case "y", "yes":
		return true
	case "n", "no":
		return false
	default:
		return defaultEnabled
	}
}

// addManually lets the user enable a provider that discovery missed (for
// example codex on an unusual path). There is nothing to "add" beyond the known
// set, so it accepts only known ids, reprompting on anything else until blank.
func addManually(scanner *bufio.Scanner, stdout io.Writer, cfg *config.Config, known []discover.Finding) {
	for {
		id := strings.ToLower(strings.TrimSpace(ask(scanner, stdout, "quotamon can read: "+knownIDs(known)+". Enter an id, or blank: ")))
		if id == "" {
			return
		}
		if !isKnownID(id, known) {
			fmt.Fprintf(stdout, "Unknown provider %q\n", id)
			continue
		}
		provider := cfg.Providers[id]
		provider.Enabled = true
		cfg.Providers[id] = provider
	}
}

func ask(scanner *bufio.Scanner, stdout io.Writer, text string) string {
	fmt.Fprint(stdout, text)
	if !scanner.Scan() {
		return ""
	}
	return strings.TrimSpace(scanner.Text())
}

func supported(findings []discover.Finding) []discover.Finding {
	known := make([]discover.Finding, 0, len(findings))
	for _, finding := range findings {
		if finding.Supported {
			known = append(known, finding)
		}
	}
	return known
}

func knownIDs(findings []discover.Finding) string {
	ids := make([]string, 0, len(findings))
	for _, finding := range findings {
		ids = append(ids, finding.ID)
	}
	return strings.Join(ids, ", ")
}

func isKnownID(id string, findings []discover.Finding) bool {
	for _, finding := range findings {
		if finding.ID == id {
			return true
		}
	}
	return false
}
