#!/usr/bin/env bash
# Regenerate the README screenshots (sample data only — never a live account).
#   docs/console.png            the `quotamon` table (via `quotamon --demo`)
#   docs/app-light.png / -dark  the menu-bar panel (via the app's QUOTA_MONITOR_RENDER path)
# Needs: Go, xcodegen, Xcode. Run from anywhere.
set -euo pipefail
cd "$(dirname "$0")/.."

echo "→ console.png"
make -C core build >/dev/null
python3 scripts/screenshot-console.py

echo "→ app-light.png / app-dark.png"
xcodegen generate >/dev/null
xcodebuild -project QuotaMonitor.xcodeproj -scheme QuotaMonitor \
  -configuration Debug -derivedDataPath .build/xcode build >/dev/null
APP="$(find .build/xcode -name 'Quota Monitor.app' -maxdepth 6 | head -1)"
QUOTA_MONITOR_RENDER="$PWD/docs/app" "$APP/Contents/MacOS/Quota Monitor"
# the render also emits docs/app-menubar-{light,dark}.png; keep only the light one as docs/menubar.png
mv -f docs/app-menubar-light.png docs/menubar.png 2>/dev/null || true
rm -f docs/app-menubar-dark.png

echo "done: docs/console.png docs/app-light.png docs/app-dark.png"
