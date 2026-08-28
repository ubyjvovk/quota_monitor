#!/usr/bin/env bash
#
# Points Claude Code's statusLine at the mirror script, so Quota Monitor gets a
# Claude reading without touching your OAuth token.
#
# Safe to re-run. Refuses to overwrite a statusLine you already configured.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MIRROR_SCRIPT="$SCRIPT_DIR/claude-statusline-mirror.sh"
SETTINGS="${CLAUDE_SETTINGS:-$HOME/.claude/settings.json}"

command -v jq >/dev/null 2>&1 || { echo "error: jq is required" >&2; exit 1; }
[ -f "$MIRROR_SCRIPT" ] || { echo "error: missing $MIRROR_SCRIPT" >&2; exit 1; }
chmod +x "$MIRROR_SCRIPT"

mkdir -p "$(dirname "$SETTINGS")"
[ -f "$SETTINGS" ] || echo '{}' >"$SETTINGS"

EXISTING=$(jq -r '.statusLine.command // empty' "$SETTINGS")
if [ -n "$EXISTING" ] && [ "$EXISTING" != "$MIRROR_SCRIPT" ]; then
  cat >&2 <<EOF
error: a statusLine is already configured:
    $EXISTING

Leaving it alone. To keep both, add this line to your own script:

    printf '%s' "\$INPUT" | jq -c '{observed_at: (now|todate), rate_limits: .rate_limits}' \\
      > "\$HOME/.quota-monitor/claude-usage.json"
EOF
  exit 1
fi

BACKUP="$SETTINGS.bak.$(date +%Y%m%d%H%M%S)"
cp "$SETTINGS" "$BACKUP"

TMP=$(mktemp)
jq --arg cmd "$MIRROR_SCRIPT" \
   '.statusLine = {type: "command", command: $cmd, padding: 0}' \
   "$SETTINGS" >"$TMP"
mv -f "$TMP" "$SETTINGS"

echo "Installed statusLine -> $MIRROR_SCRIPT"
echo "Backup of previous settings: $BACKUP"
echo
echo "Claude Code writes \$HOME/.quota-monitor/claude-usage.json on its next render."
echo "Open a Claude Code session, then re-run: swift run quotactl"
