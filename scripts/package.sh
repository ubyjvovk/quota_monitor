#!/usr/bin/env bash
#
# Package a built Quota Monitor app plus the standalone quotamon CLI binaries
# into a distributable staging set. This is exactly what CI runs at release
# time in .github/workflows/release.yml, and it can be run by hand:
#
#   bash scripts/package.sh "/path/to/Quota Monitor.app" <version> <outdir>
#   bash scripts/package.sh "/path/to/Quota Monitor.app" <version> <outdir> --sign "Developer ID Application"
#
# Outputs (written to <outdir>):
#   Quota-Monitor-<version>-universal.dmg  — a disk image of the .app
#   quotamon-<os>-<arch> x4                — standalone CLI binaries for Linux + macOS
#   SHA256SUMS                             — checksums of every file in <outdir>
#
# --sign is optional and only meaningful with a Developer ID. When given, the
# bundle is signed (hardened runtime, timestamped) from the inside out so that
# notarization can be stapled afterwards. Ad-hoc (unsigned) bundles still
# package fine and are what CI produces when no signing secrets are present.

set -euo pipefail

usage() {
  echo "usage: $0 <path/to/Quota Monitor.app> <version> <outdir> [--sign \"<identity>\"]" >&2
  exit 1
}

APP="${1:-}"; VERSION="${2:-}"; OUTDIR="${3:-}"
[ -n "$APP" ] && [ -n "$VERSION" ] && [ -n "$OUTDIR" ] || usage
[ -d "$APP" ] || { echo "error: app bundle not found: $APP" >&2; exit 1; }

SIGN=""
if [ "${4:-}" = "--sign" ]; then
  SIGN="${5:-}"
  [ -n "$SIGN" ] || usage
  [ "${6:-}" = "" ] || usage
elif [ -n "${4:-}" ]; then
  usage
fi

mkdir -p "$OUTDIR"
STAGING="$OUTDIR/staging"
rm -rf "$STAGING"
mkdir -p "$STAGING"

# --- codesign (optional) -----------------------------------------------------
# Sign from the inside out so the outermost signature covers everything the
# app loads at runtime: the bundled core executable, the widget plugin(s),
# then the bundle itself. Hardened runtime + timestamping are required for
# notarization.
if [ -n "$SIGN" ]; then
  echo "==> Signing with '$SIGN'"
  codesign --force --options runtime --timestamp \
    --preserve-metadata=entitlements --sign "$SIGN" \
    "$APP/Contents/Resources/quotamon"
  for appex in "$APP"/Contents/PlugIns/*.appex; do
    [ -e "$appex" ] || continue
    codesign --force --options runtime --timestamp \
      --preserve-metadata=entitlements --sign "$SIGN" "$appex"
  done
  codesign --force --options runtime --timestamp \
    --preserve-metadata=entitlements --sign "$SIGN" "$APP"
  codesign --verify --deep --strict "$APP"
  echo "==> Verification ok"
fi

# --- disk image --------------------------------------------------------------
echo "==> Staging"
cp -R "$APP" "$STAGING/"
ln -s /Applications "$STAGING/Applications"

echo "==> Creating DMG"
hdiutil create -volname "Quota Monitor" -srcfolder "$STAGING" \
  -ov -format UDZO "$OUTDIR/Quota-Monitor-$VERSION-universal.dmg"
rm -rf "$STAGING"

# --- standalone CLI binaries + checksums -------------------------------------
echo "==> Copying quotamon binaries"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cp "$ROOT"/core/bin/quotamon-* "$OUTDIR/"

echo "==> Writing SHA256SUMS"
(
  cd "$OUTDIR"
  shasum -a 256 * > SHA256SUMS
)

echo "==> Done:"
ls -1 "$OUTDIR"
