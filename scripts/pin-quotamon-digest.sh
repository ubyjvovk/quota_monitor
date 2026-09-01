#!/usr/bin/env bash
#
# Pin the SHA-256 digests of the core binaries the Omarchy plugin ships with.
#
#   scripts/pin-quotamon-digest.sh   pin the digests for $(cat VERSION)
#
# The plugin verifies its download against the digests committed here rather
# than against a SHA256SUMS fetched beside the binary: both would come from the
# same GitHub release, so whoever controls the release controls both. Digests
# that live in reviewed plugin code force an attacker to also push to the plugin
# repository, which leaves a commit a human can see.
#
# The checksums do not exist until after the tag — release.sh pushes v<VERSION>,
# which triggers the build that produces the binaries — so this runs *after* the
# main release is built and *before* scripts/publish-omarchy-plugin.sh. It never
# commits: the maintainer reviews the two-line diff, and that review is the
# point of the whole mechanism.

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

if [[ $# -gt 0 ]]; then
  echo "usage: scripts/pin-quotamon-digest.sh" >&2
  exit 2
fi

[ -f "$ROOT/VERSION" ] || {
  echo "error: VERSION file is missing; run scripts/set-version.sh <YYYY.M.MICRO>" >&2
  exit 1; }
VERSION="$(cat "$ROOT/VERSION")"
[ -n "$VERSION" ] || {
  echo "error: VERSION file is empty; run scripts/set-version.sh <YYYY.M.MICRO>" >&2
  exit 1; }

tmp_dir=$(mktemp -d)
trap 'rm -rf -- "$tmp_dir"' EXIT

if ! curl -fsSL \
  "https://github.com/ubyjvovk/quota_monitor/releases/download/v$VERSION/SHA256SUMS" \
  -o "$tmp_dir/SHA256SUMS"; then
  echo "release v$VERSION has no SHA256SUMS yet — run scripts/release.sh and wait for the build" >&2
  exit 1
fi

# Read the lines with the installer's own strict pattern, so anything accepted
# here is something fetch-quotamon.sh can verify against. A half-pin is worse
# than none, so a missing or duplicated asset aborts before anything is written.
pinned=""
for asset in quotamon-linux-amd64 quotamon-linux-arm64; do
  pattern="^[[:xdigit:]]{64}[[:space:]]+\\*?$asset\$"
  matches=$(grep -cE "$pattern" "$tmp_dir/SHA256SUMS" || true)
  if [[ $matches -ne 1 ]]; then
    echo "release v$VERSION has $matches checksum lines for $asset (expected 1); nothing was written" >&2
    exit 1
  fi
  line=$(grep -E "$pattern" "$tmp_dir/SHA256SUMS")
  pinned+="${line%%[[:space:]]*}  $asset"$'\n'
done

# Exactly one sidecar may exist afterwards: that is what lets the installer
# answer "is this version pinned?" by file existence alone, with no parsing.
rm -f -- "$ROOT"/omarchy/quotamon-*.sha256
pin_file="$ROOT/omarchy/quotamon-$VERSION.sha256"
printf '%s' "$pinned" > "$pin_file"

echo "Wrote omarchy/quotamon-$VERSION.sha256 — review the diff and commit it:"
printf '%s' "$pinned"
