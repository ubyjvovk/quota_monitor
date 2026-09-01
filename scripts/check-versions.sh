#!/usr/bin/env bash
#
# Verify every packaged component carries the VERSION CalVer YYYY.M.MICRO.
# This is a pure consistency check: it reads files and never changes them.

set -u

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VERSION_FILE="$ROOT/VERSION"
PROJECT_FILE="$ROOT/project.yml"
MANIFEST_FILE="$ROOT/omarchy/manifest.json"

if [ ! -f "$VERSION_FILE" ]; then
  echo "VERSION: expected CalVer YYYY.M.MICRO, found missing" >&2
  exit 1
fi

EXPECTED="$(cat "$VERSION_FILE")"
if [[ ! "$EXPECTED" =~ ^[0-9]{4}\.(1[0-2]|[1-9])\.[0-9]+$ ]]; then
  echo "VERSION: expected CalVer YYYY.M.MICRO, found '$EXPECTED'" >&2
  exit 1
fi

rc=0

if [ ! -f "$PROJECT_FILE" ]; then
  echo "project.yml: expected $EXPECTED, found missing" >&2
  rc=1
else
  PROJECT_VERSION="$(awk '
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
  ' "$PROJECT_FILE")" || PROJECT_VERSION="<missing or malformed>"
  if [ "$PROJECT_VERSION" != "$EXPECTED" ]; then
    echo "project.yml: expected $EXPECTED, found $PROJECT_VERSION" >&2
    rc=1
  fi
fi

if [ ! -f "$MANIFEST_FILE" ]; then
  echo "omarchy/manifest.json: expected $EXPECTED, found missing" >&2
  rc=1
else
  MANIFEST_VERSION="$(awk '
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
  ' "$MANIFEST_FILE")" || MANIFEST_VERSION="<missing or malformed>"
  if [ "$MANIFEST_VERSION" != "$EXPECTED" ]; then
    echo "omarchy/manifest.json: expected $EXPECTED, found $MANIFEST_VERSION" >&2
    rc=1
  fi
fi

exit "$rc"
