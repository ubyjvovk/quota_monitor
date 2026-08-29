#!/usr/bin/env bash
# Compact board summary for the PM. Bounded output: full listings for active
# lanes, count + recent for done/.
set -u
cd "$(dirname "$0")/../.." || exit 1
TIGERTEAM_DIR="${TIGERTEAM_DIR:-.tigerteam}"
BOARD="$TIGERTEAM_DIR/board"

fm_field() { sed -n "s/^$1:[[:space:]]*//p" "$2" | head -n 1 | tr -d '\r'; }

count() { find "$BOARD/$1" -maxdepth 1 -name 'T-*.md' 2>/dev/null | wc -l | tr -d ' '; }

line_for() {
  local f="$1" extra="${2:-}"
  printf '  %s [%s/%s] %s%s\n' \
    "$(basename "${f%.md}")" \
    "$(fm_field priority "$f")" \
    "$(fm_field complexity "$f")" \
    "$(fm_field title "$f")" \
    "$extra"
}

echo "board: todo $(count todo) | doing $(count doing) | blocked $(count blocked) | review $(count review) | done $(count done)"
[ -f "$TIGERTEAM_DIR/STOP" ] && echo "!! STOP file present — workers will not claim"

for lane in doing review blocked todo; do
  n="$(count "$lane")"
  [ "$n" = "0" ] && continue
  echo "$lane/:"
  for f in "$BOARD/$lane"/T-*.md; do
    [ -e "$f" ] || continue
    extra=""
    if [ "$lane" = "doing" ]; then
      claimer="$(sed -n 's/^> claimed_by: \([^ ]*\).*/\1/p' "$f" | tail -n 1)"
      [ -n "$claimer" ] && extra="  <- $claimer"
      mt="$(stat -c %Y "$f" 2>/dev/null || stat -f %m "$f" 2>/dev/null)"
      [ -n "$mt" ] && extra="$extra ($(( ($(date +%s) - mt) / 60 ))m)"
      cpid="$(grep '^> claimed_by:' "$f" | tail -n 1 | sed -n 's/.*(pid \([0-9]\{1,\}\)).*/\1/p')"
      if [ -n "$cpid" ] && ! kill -0 "$cpid" 2>/dev/null; then extra="$extra !CLAIMANT-DEAD"; fi
    fi
    if [ "$lane" = "blocked" ]; then
      grep -q '^## Questions' "$f" && extra="  (has questions)"
      grep -q '^> INCOMPLETE' "$f" && extra="$extra (failed attempts)"
    fi
    line_for "$f" "$extra"
  done
done

n="$(count done)"
if [ "$n" != "0" ]; then
  echo "done/: $n accepted, most recent:"
  # shellcheck disable=SC2012
  ls -t "$BOARD/done"/T-*.md 2>/dev/null | head -n 3 | while read -r f; do line_for "$f"; done
fi

if git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  wt="$(git worktree list 2>/dev/null | grep -c "$TIGERTEAM_DIR/worktrees/" || true)"
  [ "${wt:-0}" != "0" ] && echo "worktrees: $wt active under $TIGERTEAM_DIR/worktrees/ (unmerged ticket branches)"
fi
