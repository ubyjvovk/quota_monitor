#!/usr/bin/env bash
#
# Regenerate the Xcode project, build, and relaunch the menu bar app.
#
#   ./scripts/build.sh          build and relaunch
#   ./scripts/build.sh --clean  wipe DerivedData first

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

command -v xcodegen >/dev/null 2>&1 || {
  echo "error: xcodegen not found — brew install xcodegen" >&2; exit 1; }

[ "${1:-}" = "--clean" ] && rm -rf build

echo "==> Generating project"
xcodegen generate >/dev/null

echo "==> Stopping any running instance"
pkill -f "Quota Monitor.app" 2>/dev/null || true
sleep 1

echo "==> Building"
xcodebuild -project QuotaMonitor.xcodeproj -scheme QuotaMonitor \
  -configuration Debug -derivedDataPath build build 2>&1 \
  | grep -E "error:|warning: .*(deprecated|unused)|BUILD SUCCEEDED|BUILD FAILED" || true

APP="$ROOT/build/Build/Products/Debug/Quota Monitor.app"
[ -d "$APP" ] || { echo "error: build produced no app bundle" >&2; exit 1; }

echo "==> Launching $APP"
open "$APP"
echo
echo "Quota Monitor is now in your menu bar. Click it for the panel."
echo "To install into /Applications:  cp -R \"$APP\" /Applications/"
