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

	"quotamon/internal/hybrid"
	"quotamon/internal/registry"
	"quotamon/internal/snapshot"
	"quotamon/internal/source"
)

const usageText = `Usage: quotamon <command> [--no-live]

Commands:
  snapshot  Print the normalized quota snapshot as JSON
  waybar    Print a one-line Waybar custom-module payload
  check     Probe each provider source independently

Options:
  --no-live  Skip live sources
  --help, -h Show this help and exit
`

const overallTimeout = 10 * time.Second

type providerFactory func(registry.Options) []hybrid.Provider

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, time.Now))
}

func run(args []string, stdout, stderr io.Writer, now func() time.Time) int {
	return runWithFactory(args, stdout, stderr, now, registry.All)
}

func runWithFactory(args []string, stdout, stderr io.Writer, now func() time.Time, providers providerFactory) int {
	command, liveEnabled, help, valid := parseArguments(args)
	if help {
		fmt.Fprint(stdout, usageText)
		return 0
	}
	if !valid {
		fmt.Fprint(stderr, usageText)
		return 2
	}

	configured := providers(registry.Options{LiveEnabled: func(string) bool { return liveEnabled }})
	ctx, cancel := context.WithTimeout(context.Background(), overallTimeout)
	defer cancel()

	switch command {
	case "snapshot":
		result := fetchAll(ctx, configured, now)
		encoded, err := result.Encode()
		if err != nil {
			fmt.Fprintf(stderr, "encode snapshot: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, string(encoded))
		for _, provider := range result.Providers {
			if len(provider.Windows) > 0 {
				return 0
			}
		}
		return 1
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

func parseArguments(args []string) (command string, liveEnabled, help, valid bool) {
	if len(args) == 0 {
		return "", true, true, true
	}
	for _, argument := range args {
		if argument == "--help" || argument == "-h" {
			return "", true, true, true
		}
	}
	if args[0] != "snapshot" && args[0] != "waybar" && args[0] != "check" {
		return "", true, false, false
	}
	if len(args) == 1 {
		return args[0], true, false, true
	}
	if len(args) == 2 && args[1] == "--no-live" {
		return args[0], false, false, true
	}
	return "", true, false, false
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
}

func probeAll(ctx context.Context, providers []hybrid.Provider) []diagnosticResult {
	probes := make([]diagnosticProbe, 0, len(providers)*2)
	for _, provider := range providers {
		if provider.Local != nil {
			probes = append(probes, diagnosticProbe{providerName: provider.DisplayName, origin: "local", source: provider.Local})
		}
		if provider.LiveEnabled && provider.Live != nil {
			probes = append(probes, diagnosticProbe{providerName: provider.DisplayName, origin: "live", source: provider.Live})
		}
	}

	results := make([]diagnosticResult, len(probes))
	var group sync.WaitGroup
	for index := range probes {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			provider, err := probes[index].source.Fetch(ctx)
			results[index] = diagnosticResult{diagnosticProbe: probes[index], provider: provider, err: err}
		}(index)
	}
	group.Wait()
	return results
}

func writeDiagnostics(writer io.Writer, results []diagnosticResult) {
	for _, result := range results {
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
