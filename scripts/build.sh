#!/usr/bin/env bash
#
# Regenerate the Xcode project, build, and relaunch the menu bar app.
#
#   ./scripts/build.sh             build and relaunch
#   ./scripts/build.sh --clean     wipe DerivedData first
#   ./scripts/build.sh --no-open   build only, do not launch

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

# Local builds stamp the CalVer from VERSION, the same value release.sh tags.
[ -f "$ROOT/VERSION" ] || {
  echo "error: VERSION file is missing; run scripts/set-version.sh <YYYY.M.MICRO>" >&2
  exit 1; }
VERSION="$(cat "$ROOT/VERSION")"
[ -n "$VERSION" ] || {
  echo "error: VERSION file is empty; run scripts/set-version.sh <YYYY.M.MICRO>" >&2
  exit 1; }

APP="$ROOT/build/Build/Products/Debug/Quota Monitor.app"

# Remove the previous bundle before building. Without this a compile failure
# leaves the last good .app in place, and every check below — and `open` — would
# happily accept it, so you relaunch stale code believing the build worked.
rm -rf "$APP"

echo "==> Building"
# The grep filter swallows xcodebuild's exit status: `| grep ... || true` reports
# grep's result, and grep exits 1 merely because nothing matched. Read
# xcodebuild's own status out of PIPESTATUS instead. (`set -e` is lifted for the
# pipeline only, so pipefail cannot abort before PIPESTATUS is captured.)
set +e
xcodebuild -project QuotaMonitor.xcodeproj -scheme QuotaMonitor \
  -configuration Debug -derivedDataPath build \
  MARKETING_VERSION="$VERSION" build 2>&1 \
  | grep -E "error:|warning: .*(deprecated|unused)|BUILD SUCCEEDED|BUILD FAILED"
build_status=${PIPESTATUS[0]}
set -e
[ "$build_status" -eq 0 ] || {
  echo "error: xcodebuild failed (exit $build_status) — rerun the xcodebuild line without the grep filter for the full log" >&2
  exit "$build_status"; }

[ -d "$APP" ] || { echo "error: build produced no app bundle" >&2; exit 1; }

# The app has no fetchers of its own; without the core it shows nothing at all,
# so an app bundle missing it is a failed build, not a degraded one.
[ -x "$APP/Contents/Resources/quotamon" ] || {
  echo "error: app bundle has no executable Contents/Resources/quotamon" >&2; exit 1; }

if [ "${1:-}" = "--no-open" ]; then
  echo "==> Built $APP"
  exit 0
fi

echo "==> Launching $APP"
open "$APP"
echo
echo "Quota Monitor is now in your menu bar. Click it for the panel."
echo "To install into /Applications:  cp -R \"$APP\" /Applications/"
