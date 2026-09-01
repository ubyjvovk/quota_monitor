#!/usr/bin/env bash
#
# Set every packaged component to one CalVer YYYY.M.MICRO version.
# This is the only supported way to bump VERSION and its required literals.

set -euo pipefail

usage() {
  echo "usage: $0 <YYYY.M.MICRO> (CalVer: four-digit year, month 1-12, monthly release number)" >&2
  exit 2
}

[ "$#" -eq 1 ] || usage
NEW_VERSION="$1"
[[ "$NEW_VERSION" =~ ^[0-9]{4}\.(1[0-2]|[1-9])\.[0-9]+$ ]] || usage

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VERSION_FILE="$ROOT/VERSION"
PROJECT_FILE="$ROOT/project.yml"
MANIFEST_FILE="$ROOT/omarchy/manifest.json"

for target in "$VERSION_FILE" "$PROJECT_FILE" "$MANIFEST_FILE"; do
  [ -f "$target" ] || {
    echo "error: version target is missing: ${target#"$ROOT"/}" >&2
    exit 1
  }
done

project_version() {
  awk '
    /^[[:space:]]*MARKETING_VERSION:[[:space:]]*"[^"]*"[[:space:]]*$/ {
      count++
      value = $0
      sub(/^[^:]*:[[:space:]]*"/, "", value)
      sub(/"[[:space:]]*$/, "", value)
    }
    END {
      if (count != 1) exit 1
      print value
    }
  ' "$PROJECT_FILE"
}

manifest_version() {
  awk '
    /^  "version":[[:space:]]*"[^"]*",[[:space:]]*$/ {
      count++
      value = $0
      sub(/^  "version":[[:space:]]*"/, "", value)
      sub(/",[[:space:]]*$/, "", value)
    }
    END {
      if (count != 1) exit 1
      print value
    }
  ' "$MANIFEST_FILE"
}

OLD_VERSION="$(cat "$VERSION_FILE")"
if ! OLD_PROJECT_VERSION="$(project_version)"; then
  echo "error: project.yml must contain exactly one quoted MARKETING_VERSION" >&2
  exit 1
fi
if ! OLD_MANIFEST_VERSION="$(manifest_version)"; then
  echo "error: omarchy/manifest.json must contain exactly one top-level version key" >&2
  exit 1
fi

TMP_FILE=""
cleanup() {
  [ -z "$TMP_FILE" ] || rm -f "$TMP_FILE"
}
trap cleanup EXIT

rewrite_version_file() {
  [ "$OLD_VERSION" != "$NEW_VERSION" ] || return 0
  TMP_FILE="$(mktemp "$ROOT/.set-version.XXXXXX")"
  cp -p "$VERSION_FILE" "$TMP_FILE"
  printf '%s\n' "$NEW_VERSION" > "$TMP_FILE"
  cmp -s "$VERSION_FILE" "$TMP_FILE" && {
    echo "error: VERSION rewrite produced no change" >&2
    exit 1
  }
  mv "$TMP_FILE" "$VERSION_FILE"
  TMP_FILE=""
}

rewrite_project_file() {
  [ "$OLD_PROJECT_VERSION" != "$NEW_VERSION" ] || return 0
  TMP_FILE="$(mktemp "$ROOT/.set-version.XXXXXX")"
  cp -p "$PROJECT_FILE" "$TMP_FILE"
  awk -v version="$NEW_VERSION" '
    /^[[:space:]]*MARKETING_VERSION:[[:space:]]*"[^"]*"[[:space:]]*$/ {
      sub(/"[^"]*"/, "\"" version "\"")
      count++
    }
    { print }
    END { if (count != 1) exit 1 }
  ' "$PROJECT_FILE" > "$TMP_FILE" || {
    echo "error: failed to rewrite MARKETING_VERSION in project.yml" >&2
    exit 1
  }
  cmp -s "$PROJECT_FILE" "$TMP_FILE" && {
    echo "error: project.yml rewrite produced no change" >&2
    exit 1
  }
  mv "$TMP_FILE" "$PROJECT_FILE"
  TMP_FILE=""
}

rewrite_manifest_file() {
  [ "$OLD_MANIFEST_VERSION" != "$NEW_VERSION" ] || return 0
  TMP_FILE="$(mktemp "$ROOT/.set-version.XXXXXX")"
  cp -p "$MANIFEST_FILE" "$TMP_FILE"
  awk -v version="$NEW_VERSION" '
    /^  "version":[[:space:]]*"[^"]*",[[:space:]]*$/ {
      sub(/"[^"]*",/, "\"" version "\",")
      count++
    }
    { print }
    END { if (count != 1) exit 1 }
  ' "$MANIFEST_FILE" > "$TMP_FILE" || {
    echo "error: failed to rewrite the top-level version in omarchy/manifest.json" >&2
    exit 1
  }
  cmp -s "$MANIFEST_FILE" "$TMP_FILE" && {
    echo "error: omarchy/manifest.json rewrite produced no change" >&2
    exit 1
  }
  mv "$TMP_FILE" "$MANIFEST_FILE"
  TMP_FILE=""
}

rewrite_version_file
rewrite_project_file
rewrite_manifest_file

if [ "$OLD_VERSION" = "$NEW_VERSION" ]; then
  echo "VERSION: $OLD_VERSION -> $NEW_VERSION (no change)"
else
  echo "VERSION: $OLD_VERSION -> $NEW_VERSION"
fi
if [ "$OLD_PROJECT_VERSION" = "$NEW_VERSION" ]; then
  echo "project.yml: $OLD_PROJECT_VERSION -> $NEW_VERSION (no change)"
else
  echo "project.yml: $OLD_PROJECT_VERSION -> $NEW_VERSION"
fi
if [ "$OLD_MANIFEST_VERSION" = "$NEW_VERSION" ]; then
  echo "omarchy/manifest.json: $OLD_MANIFEST_VERSION -> $NEW_VERSION (no change)"
else
  echo "omarchy/manifest.json: $OLD_MANIFEST_VERSION -> $NEW_VERSION"
fi
