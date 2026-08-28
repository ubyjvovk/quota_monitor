#!/usr/bin/env bash
#
# Claude Code statusLine command that does double duty:
#   1. mirrors the `rate_limits` block to disk for Quota Monitor to read
#   2. prints a status line
#
# Claude Code passes its status payload as JSON on stdin and renders whatever we
# print on stdout. `rate_limits` is part of that documented payload, so this needs
# no tokens and makes no network calls.
#
# Runs on every status render — keep it fast and never let it fail the line.

set -uo pipefail

INPUT=$(cat)
MIRROR_DIR="${QUOTA_MONITOR_DIR:-$HOME/.quota-monitor}"
MIRROR="$MIRROR_DIR/claude-usage.json"

if ! command -v jq >/dev/null 2>&1; then
  printf 'Quota Monitor: jq not found'
  exit 0
fi

# --- 1. Mirror -------------------------------------------------------------
# Only write when rate_limits is actually present; it is absent until the first
# API response of a session, and clobbering a good file with an empty one would
# make the widget forget known-good numbers.
if printf '%s' "$INPUT" | jq -e '.rate_limits != null' >/dev/null 2>&1; then
  mkdir -p "$MIRROR_DIR"
  if printf '%s' "$INPUT" \
    | jq -c --arg ts "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
        '{observed_at: $ts, rate_limits: .rate_limits}' >"$MIRROR.tmp" 2>/dev/null
  then
    # Atomic, so a widget reading mid-write never sees a partial file.
    mv -f "$MIRROR.tmp" "$MIRROR"
  else
    rm -f "$MIRROR.tmp"
  fi
fi

# --- 2. Status line --------------------------------------------------------
printf '%s' "$INPUT" | jq -r '
  def pct($v): if $v == null then empty else "\($v | round)%" end;

  [ (.model.display_name // empty),
    (.workspace.current_dir // .cwd // "" | split("/") | last | select(. != "")),
    (pct(.rate_limits.five_hour.used_percentage)      | "5h " + .),
    (pct(.rate_limits.seven_day.used_percentage)      | "wk " + .),
    (pct(.rate_limits.seven_day_opus.used_percentage) | "opus " + .)
  ]
  | map(select(. != null and . != ""))
  | join("  ·  ")
' 2>/dev/null || printf 'Quota Monitor'
