// Command quotamon emits portable quota snapshots for frontend consumers.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"quotamon/internal/config"
	"quotamon/internal/discover"
	"quotamon/internal/hybrid"
	"quotamon/internal/registry"
	"quotamon/internal/snapshot"
	"quotamon/internal/source"
)

const usageText = `Usage: quotamon [--no-live]
       quotamon --json [--no-live]
       quotamon <command> [--no-live]

Commands:
  snapshot  Print the normalized quota snapshot as JSON
  waybar    Print a one-line Waybar custom-module payload
  check     Probe each provider source independently
  setup     Configure providers interactively; --yes enables what was found
  providers List providers and whether each is enabled

Options:
  --no-live  Skip live sources
  --help, -h Show this help and exit
`

const overallTimeout = 10 * time.Second

type providerFactory func(registry.Options) []hybrid.Provider

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, time.Now))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer, now func() time.Time) int {
	return runWithFactory(args, stdin, stdout, stderr, now, registry.All)
}

func runWithFactory(args []string, stdin io.Reader, stdout, stderr io.Writer, now func() time.Time, providers providerFactory) int {
	command, liveEnabled, yes, help, valid := parseArguments(args)
	if help {
		fmt.Fprint(stdout, usageText)
		return 0
	}
	if !valid {
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

	configured := providers(registry.Options{LiveEnabled: func(string) bool { return liveEnabled }, Config: cfg})
	ctx, cancel := context.WithTimeout(context.Background(), overallTimeout)
	defer cancel()

	switch command {
	case "table":
		result := fetchAll(ctx, configured, now)
		fmt.Fprintln(stdout, renderTable(result, result.GeneratedAt.Time))
		return windowExitStatus(result)
	case "snapshot":
		result := fetchAll(ctx, configured, now)
		encoded, err := result.Encode()
		if err != nil {
			fmt.Fprintf(stderr, "encode snapshot: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, string(encoded))
		return windowExitStatus(result)
	case "waybar":
		result := fetchAll(ctx, configured, now)
		if err := json.NewEncoder(stdout).Encode(renderWaybar(result, result.GeneratedAt.Time)); err != nil {
			fmt.Fprintf(stderr, "encode Waybar payload: %v\n", err)
			return 1
		}
		return 0
	case "check":
		writeDiagnostics(stdout, probeAll(ctx, configured))
		return 0
	default:
		panic("validated command was not handled")
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

func parseArguments(args []string) (command string, liveEnabled, yes, help, valid bool) {
	if len(args) == 0 {
		return "table", true, false, false, true
	}
	for _, argument := range args {
		if argument == "--help" || argument == "-h" {
			return "", true, false, true, true
		}
	}
	if len(args) == 1 && args[0] == "--no-live" {
		return "table", false, false, false, true
	}
	if args[0] == "--json" {
		if len(args) == 1 {
			return "snapshot", true, false, false, true
		}
		if len(args) == 2 && args[1] == "--no-live" {
			return "snapshot", false, false, false, true
		}
		return "", true, false, false, false
	}
	if !isKnownCommand(args[0]) {
		return "", true, false, false, false
	}
	if len(args) == 1 {
		return args[0], true, false, false, true
	}
	if args[0] == "setup" && len(args) == 2 && args[1] == "--yes" {
		return "setup", true, true, false, true
	}
	if len(args) == 2 && args[1] == "--no-live" {
		return args[0], false, false, false, true
	}
	return "", true, false, false, false
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

func fetchAll(ctx context.Context, providers []hybrid.Provider, now func() time.Time) snapshot.Snapshot {
	resolved := make([]snapshot.Provider, len(providers))
	var group sync.WaitGroup
	for index := range providers {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			resolved[index] = providers[index].Fetch(ctx)
		}(index)
	}
	group.Wait()
	return snapshot.Snapshot{
		Providers:   resolved,
		GeneratedAt: snapshot.Time{Time: now()},
	}
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
	// skipped marks a live source disabled by --no-live; such a result has no
	// provider or error and renders a discoverable placeholder instead of probing.
	skipped bool
}

func probeAll(ctx context.Context, providers []hybrid.Provider) []diagnosticResult {
	probes := make([]diagnosticProbe, 0, len(providers)*2)
	for _, provider := range providers {
		if provider.Local != nil {
			probes = append(probes, diagnosticProbe{providerName: provider.DisplayName, origin: "local", source: provider.Local})
		}
		if provider.Live != nil && provider.LiveEnabled {
			probes = append(probes, diagnosticProbe{providerName: provider.DisplayName, origin: "live", source: provider.Live})
		}
	}

	results := make([]diagnosticResult, 0, len(probes)+len(providers))
	probed := make([]diagnosticResult, len(probes))
	var group sync.WaitGroup
	for index := range probes {
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

func writeDiagnostics(writer io.Writer, results []diagnosticResult) {
	for _, result := range results {
		if result.skipped {
			fmt.Fprintf(writer, "%s %s: skipped (--no-live)\n", result.providerName, result.origin)
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
		fmt.Fprintf(writer, "%s %s: ok — %d windows, %s\n", result.providerName, result.origin, len(result.provider.Windows), plan)
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
