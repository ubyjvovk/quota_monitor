#!/usr/bin/env bash
# Runs every test suite in the repo: QuotaKit (Swift) and the Go core.
# Invoked via .tigerteam/scripts/run-tests.sh (tigerteam test_cmd); do not
# call swift/go test directly in tickets — this is the single source of truth.
set -u
cd "$(dirname "$0")/.." || exit 1

# 2026-08-31: an unsandboxed suite run deleted the real config; pin every test
# to a throwaway dir so no suite can reach ~/.config/quotamon.
if [ -z "${QUOTA_MONITOR_DIR:-}" ]; then
  QUOTA_MONITOR_DIR="$(mktemp -d "${TMPDIR:-/tmp}/quotamon-tests.XXXXXX")"
  trap 'rm -rf "$QUOTA_MONITOR_DIR"' EXIT
fi
export QUOTA_MONITOR_DIR

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

if command -v node >/dev/null 2>&1; then
  echo "### node (omarchy model)"
  node omarchy/model_test.mjs || rc=1
fi

echo "### exit $rc"
exit $rc
