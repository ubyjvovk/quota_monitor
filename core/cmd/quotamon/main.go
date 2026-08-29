// Command quotamon emits portable quota snapshots for frontend consumers.
package main

import (
	"fmt"
	"io"
	"os"
	"time"

	"quotamon/internal/snapshot"
)

const usageText = `Usage: quotamon <command>

Commands:
  snapshot  Print the normalized quota snapshot as JSON
  waybar    Print Waybar module JSON (not implemented)
  check     Print per-source diagnostics (not implemented)
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, time.Now))
}

func run(args []string, stdout, stderr io.Writer, now func() time.Time) int {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprint(stdout, usageText)
		return 0
	}

	switch args[0] {
	case "snapshot":
		if len(args) != 1 {
			fmt.Fprint(stderr, usageText)
			return 2
		}
		encoded, err := (snapshot.Snapshot{
			Providers:   []snapshot.Provider{},
			GeneratedAt: snapshot.Time{Time: now()},
		}).Encode()
		if err != nil {
			fmt.Fprintf(stderr, "encode snapshot: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, string(encoded))
		return 0
	case "waybar", "check":
		fmt.Fprintf(stderr, "%s: not implemented\n", args[0])
		return 2
	default:
		fmt.Fprint(stderr, usageText)
		return 2
	}
}
