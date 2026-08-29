#!/usr/bin/env bash
# Runs every test suite in the repo: QuotaKit (Swift) and the Go core.
# Invoked via .tigerteam/scripts/run-tests.sh (tigerteam test_cmd); do not
# call swift/go test directly in tickets — this is the single source of truth.
set -u
cd "$(dirname "$0")/.." || exit 1
rc=0

echo "### swift test (QuotaKit)"
# --disable-sandbox: SwiftPM nests its own sandbox-exec, which macOS refuses
# inside the worker Seatbelt profile.
swift test --disable-sandbox --package-path QuotaKit || rc=1

if [ -f core/go.mod ]; then
  echo "### go test (core)"
  # GOTOOLCHAIN=local: never auto-download a toolchain; GOFLAGS=-mod=mod is
  # irrelevant (stdlib only) but keeps `go` from touching the network.
  ( cd core && GOTOOLCHAIN=local GOPROXY=off go vet ./... && GOTOOLCHAIN=local GOPROXY=off go test ./... ) || rc=1
fi

echo "### exit $rc"
exit $rc
