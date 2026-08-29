// Command quotamon emits portable quota snapshots for frontend consumers.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"quotamon/internal/config"
	"quotamon/internal/discover"
	"quotamon/internal/format"
	"quotamon/internal/hybrid"
	"quotamon/internal/providers/kimi"
	"quotamon/internal/registry"
	"quotamon/internal/snapshot"
	"quotamon/internal/source"
)

const usageText = `Usage: quotamon [--no-live] [--fresh] [--color=auto|always|never]
       quotamon --json [--no-live] [--fresh] [--color=auto|always|never]
       quotamon <command> [--no-live] [--fresh] [--color=auto|always|never]

Commands:
  snapshot  Print the normalized quota snapshot as JSON
  waybar    Print a one-line Waybar custom-module payload
  check     Probe each provider source independently
  setup     Configure providers interactively; --yes enables what was found
  providers List providers and whether each is enabled

Options:
  --no-live                 Skip live sources
  --fresh                   Bypass stale-token cached readings
  --color=auto|always|never Colour the table usage bars (default: auto)
  --help, -h                Show this help and exit
`

const (
	// fetchTimeout bounds the source queries themselves.
	fetchTimeout = 10 * time.Second
	// prefetchTimeout bounds the stale-token phase separately: a Kimi refresh
	// launches the CLI and needs up to its own 20-second deadline, which must
	// not consume the fetch budget.
	prefetchTimeout = 25 * time.Second
)

type providerFactory func(registry.Options) []hybrid.Provider

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, time.Now))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer, now func() time.Time) int {
	return runWithFactory(args, stdin, stdout, stderr, now, registry.All)
}

func runWithFactory(args []string, stdin io.Reader, stdout, stderr io.Writer, now func() time.Time, providers providerFactory) int {
	args, colorMode, colorValid := parseColorArgument(args)
	command, liveEnabled, fresh, yes, help, valid := parseArguments(args)
	if help {
		fmt.Fprint(stdout, usageText)
		return 0
	}
	if !valid || !colorValid {
		fmt.Fprint(stderr, usageText)
		return 2
	}

	// setup and providers are the only commands that do not require a config:
	// setup is how the user creates one, and providers reports its state.
	switch command {
	case "setup":
		return runSetup(stdin, stdout, stderr, yes, discover.All)
	case "providers":
		return runProviders(stdout, stderr, discover.All)
	}

	cfg, err := config.Load()
	if err != nil {
		if errors.Is(err, config.ErrMissing) {
			return reportMissingConfig(command, stdout, stderr)
		}
		fmt.Fprintf(stderr, "config: %v\n", err)
		return 3
	}

	configured := providers(registry.Options{LiveEnabled: func(string) bool { return liveEnabled }, Config: cfg, Fresh: fresh})

	switch command {
	case "table":
		result := fetchAll(configured, now)
		fmt.Fprintln(stdout, renderTableWithColor(result, result.GeneratedAt.Time, tableColorEnabled(colorMode, stdout)))
		return windowExitStatus(result)
	case "snapshot":
		result := fetchAll(configured, now)
		encoded, err := result.Encode()
		if err != nil {
			fmt.Fprintf(stderr, "encode snapshot: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, string(encoded))
		return windowExitStatus(result)
	case "waybar":
		result := fetchAll(configured, now)
		if err := json.NewEncoder(stdout).Encode(renderWaybar(result, result.GeneratedAt.Time)); err != nil {
			fmt.Fprintf(stderr, "encode Waybar payload: %v\n", err)
			return 1
		}
		return 0
	case "check":
		ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
		defer cancel()
		writeDiagnostics(stdout, probeAll(ctx, configured, now()))
		return 0
	default:
		panic("validated command was not handled")
	}
}

func parseColorArgument(args []string) ([]string, string, bool) {
	filtered := make([]string, 0, len(args))
	mode := "auto"
	found := false
	for _, argument := range args {
		if !strings.HasPrefix(argument, "--color=") {
			filtered = append(filtered, argument)
			continue
		}
		if found {
			return filtered, mode, false
		}
		found = true
		mode = strings.TrimPrefix(argument, "--color=")
		switch mode {
		case "auto", "always", "never":
		default:
			return filtered, mode, false
		}
	}
	return filtered, mode, true
}

func tableColorEnabled(mode string, stdout io.Writer) bool {
	if _, disabled := os.LookupEnv("NO_COLOR"); disabled {
		return false
	}
	switch mode {
	case "always":
		return true
	case "never":
		return false
	default:
		file, ok := stdout.(*os.File)
		if !ok {
			return false
		}
		info, err := file.Stat()
		return err == nil && info.Mode()&os.ModeCharDevice != 0
	}
}

// reportMissingConfig tells the user how to create the mandatory file. The
// JSON-only frontends get a machine-readable payload; the rest get a hint on
// stderr. Exits differ because Waybar treats a missing config as "safe to
// show the run-setup message", not as a failure.
func reportMissingConfig(command string, stdout, stderr io.Writer) int {
	if command == "waybar" {
		fmt.Fprintln(stdout, "{\"text\":\"quota: run setup\",\"tooltip\":\"Run `quotamon setup` in a terminal\",\"class\":\"unavailable\",\"percentage\":0}")
		return 0
	}
	fmt.Fprintln(stderr, "No config yet — run: quotamon setup")
	return 3
}

func parseArguments(args []string) (command string, liveEnabled, fresh, yes, help, valid bool) {
	for _, argument := range args {
		if argument == "--help" || argument == "-h" {
			return "", true, false, false, true, true
		}
	}

	command = "table"
	rest := args
	if len(rest) > 0 && rest[0] == "--json" {
		command = "snapshot"
		rest = rest[1:]
	} else if len(rest) > 0 && isKnownCommand(rest[0]) {
		command = rest[0]
		rest = rest[1:]
	} else if len(rest) > 0 && rest[0] != "--no-live" && rest[0] != "--fresh" {
		return "", true, false, false, false, false
	}

	if command == "setup" && len(rest) == 1 && rest[0] == "--yes" {
		return command, true, false, true, false, true
	}
	liveEnabled = true
	for _, argument := range rest {
		switch argument {
		case "--no-live":
			if !liveEnabled {
				return "", true, false, false, false, false
			}
			liveEnabled = false
		case "--fresh":
			if fresh || (command != "table" && command != "snapshot" && command != "waybar") {
				return "", true, false, false, false, false
			}
			fresh = true
		default:
			return "", true, false, false, false, false
		}
	}
	return command, liveEnabled, fresh, false, false, true
}

func isKnownCommand(command string) bool {
	switch command {
	case "snapshot", "waybar", "check", "setup", "providers":
		return true
	default:
		return false
	}
}

func windowExitStatus(result snapshot.Snapshot) int {
	for _, provider := range result.Providers {
		if len(provider.Windows) > 0 {
			return 0
		}
	}
	return 1
}

// fetchAll resolves every provider concurrently. The stale-token phase (cache
// reads and, for Kimi, launching the CLI to renew its own sign-in) runs first
// under its own 25-second budget so a refresh cannot consume the 10-second
// fetch budget. With --no-live every PreFetch is a no-op, so the cache and
// any refresh are skipped.
func fetchAll(providers []hybrid.Provider, now func() time.Time) snapshot.Snapshot {
	prepared := preFetchAll(providers)
	ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
	defer cancel()
	resolved := make([]snapshot.Provider, len(prepared))
	var group sync.WaitGroup
	for index := range prepared {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			resolved[index] = prepared[index].Fetch(ctx)
		}(index)
	}
	group.Wait()
	return snapshot.Snapshot{
		Providers:   resolved,
		GeneratedAt: snapshot.Time{Time: now()},
	}
}

func preFetchAll(providers []hybrid.Provider) []hybrid.Prepared {
	ctx, cancel := context.WithTimeout(context.Background(), prefetchTimeout)
	defer cancel()
	prepared := make([]hybrid.Prepared, len(providers))
	var group sync.WaitGroup
	for index := range providers {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			prepared[index] = providers[index].PreFetch(ctx)
		}(index)
	}
	group.Wait()
	return prepared
}

type diagnosticProbe struct {
	providerName string
	origin       string
	source       source.Source
}

type diagnosticResult struct {
	diagnosticProbe
	provider snapshot.Provider
	err      error
	// message is a diagnostic resolved without probing the source.
	message string
	// skipped marks a live source disabled by --no-live; such a result has no
	// provider or error and renders a discoverable placeholder instead of probing.
	skipped bool
}

func probeAll(ctx context.Context, providers []hybrid.Provider, now time.Time) []diagnosticResult {
	probes := make([]diagnosticProbe, 0, len(providers)*2)
	resolved := make(map[int]string)
	for _, provider := range providers {
		if provider.Local != nil {
			probes = append(probes, diagnosticProbe{providerName: provider.DisplayName, origin: "local", source: provider.Local})
		}
		if provider.Live != nil && provider.LiveEnabled {
			probes = append(probes, diagnosticProbe{providerName: provider.DisplayName, origin: "live", source: provider.Live})
			if provider.TokenStale != nil && provider.TokenStale(now) {
				resolved[len(probes)-1] = staleTokenMessage(provider, now)
			}
		}
	}

	results := make([]diagnosticResult, 0, len(probes)+len(providers))
	probed := make([]diagnosticResult, len(probes))
	var group sync.WaitGroup
	for index := range probes {
		if message := resolved[index]; message != "" {
			probed[index] = diagnosticResult{diagnosticProbe: probes[index], message: message}
			continue
		}
		group.Add(1)
		go func(index int) {
			defer group.Done()
			provider, err := probes[index].source.Fetch(ctx)
			probed[index] = diagnosticResult{diagnosticProbe: probes[index], provider: provider, err: err}
		}(index)
	}
	group.Wait()
	results = append(results, probed...)

	// A live source disabled by --no-live is skipped, not silently dropped, so a
	// live-only provider stays discoverable in check output without ever touching
	// credentials or the network. Providers with no local source print no local line.
	for _, provider := range providers {
		if provider.Live != nil && !provider.LiveEnabled {
			results = append(results, diagnosticResult{
				diagnosticProbe: diagnosticProbe{providerName: provider.DisplayName, origin: "live"},
				skipped:         true,
			})
		}
	}
	return results
}

func staleTokenMessage(provider hybrid.Provider, now time.Time) string {
	message := "token stale"
	if provider.ID == kimi.ProviderID {
		if credentials, err := (kimi.CredentialStore{}).Load(); err == nil && credentials.ExpiresAt != nil {
			message += " (expired " + format.Age(now.Sub(*credentials.ExpiresAt)) + ")"
		}
	}
	if provider.Cache != nil {
		if cached, found := provider.Cache.Load(provider.ID); found && cacheHasCurrentWindow(cached, now) {
			message += " — cached reading available"
		}
	}
	return message
}

func cacheHasCurrentWindow(provider snapshot.Provider, now time.Time) bool {
	for _, window := range provider.Windows {
		if _, current := window.CurrentUsedPercent(now); current {
			return true
		}
	}
	return false
}

func writeDiagnostics(writer io.Writer, results []diagnosticResult) {
	for _, result := range results {
		if result.skipped {
			fmt.Fprintf(writer, "%s %s: skipped (--no-live)\n", result.providerName, result.origin)
			continue
		}
		if result.message != "" {
			fmt.Fprintf(writer, "%s %s: %s\n", result.providerName, result.origin, result.message)
			continue
		}
		if result.err != nil {
			fmt.Fprintf(writer, "%s %s: %s: %s\n", result.providerName, result.origin, errorKind(result.err), result.err)
			continue
		}
		plan := "—"
		if result.provider.Plan != nil {
			plan = *result.provider.Plan
		}
		windowWord := "windows"
		if len(result.provider.Windows) == 1 {
			windowWord = "window"
		}
		fmt.Fprintf(writer, "%s %s: ok — %d %s, %s\n", result.providerName, result.origin, len(result.provider.Windows), windowWord, plan)
	}
}

func errorKind(err error) string {
	var sourceError *source.Error
	if !errors.As(err, &sourceError) {
		return "transport"
	}
	switch sourceError.Kind {
	case source.NotConfigured:
		return "notConfigured"
	case source.NoDataFound:
		return "noDataFound"
	case source.Transport:
		return "transport"
	case source.Malformed:
		return "malformed"
	case source.Unauthorized:
		return "unauthorized"
	default:
		return "transport"
	}
}
