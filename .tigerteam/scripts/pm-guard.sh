#!/usr/bin/env bash
# PM guard — Claude Code Stop hook for tigerteam PM sessions.
#
# Fires every time the PM session is about to go idle. Blocks the stop (the
# reason is fed back to the model as its next instruction) when the board has
# actionable state the PM has not handled:
#   - tickets sitting in review/ or blocked/ older than GRACE_MIN minutes
#     (the grace period lets a digest be handled or a conversation finish
#     without nagging on every turn boundary), or
#   - (pull mode) the supervisor is running but no `tigerteam events --wait`
#     waiter is armed for THIS board (the forgot-to-re-arm stall). In push
#     mode that waiter scan is skipped; digests land in .tigerteam/digests/.
#
# Installed by `tigerteam init` as `.tigerteam/scripts/pm-guard.sh`. Wiring
# into the assistant (e.g. `.claude/settings.local.json` Stop hook) is a
# deliberate user action — init never touches `.claude/`. See README
# "Keeping the PM loop alive".
#
# PM_GUARD_ROOT overrides the board root (tests). PM_GUARD_GRACE_MIN overrides
# the grace period (default 5). PM_GUARD_PUSH overrides push-mode detection
# (tests; `true`/`false`). When empty, the script runs
# `tigerteam config get pm.push_digests` (missing CLI → false).
set -u

GRACE_MIN="${PM_GUARD_GRACE_MIN:-5}"
if [ -n "${PM_GUARD_ROOT:-}" ]; then
  ROOT="$PM_GUARD_ROOT"
else
  # in-container.sh idiom: board root is two levels above this script when
  # installed at .tigerteam/scripts/pm-guard.sh.
  cd "$(dirname "$0")/../.." 2>/dev/null || exit 0
  ROOT="$(pwd -P)"
fi
cd "$ROOT" 2>/dev/null || exit 0
ROOT="$(pwd -P)"
[ -d .tigerteam/board ] || exit 0

# Push-mode detection (after ROOT is resolved). Anything other than true → false.
push="${PM_GUARD_PUSH:-}"
if [ -z "$push" ]; then
  # Hook environments may lack ~/.local/bin on PATH; a guard that cannot find
  # the CLI must not silently degrade to pull mode and nag for an impossible
  # re-arm, so fall back to the tool-install location before giving up.
  tt="$(command -v tigerteam 2>/dev/null || true)"
  [ -z "$tt" ] && [ -x "$HOME/.local/bin/tigerteam" ] && tt="$HOME/.local/bin/tigerteam"
  if [ -n "$tt" ]; then
    push="$("$tt" config get pm.push_digests 2>/dev/null || echo false)"
  else
    push=false
  fi
fi
push="$(printf '%s' "$push" | tr '[:upper:]' '[:lower:]')"
if [ "$push" != "true" ]; then
  push=false
fi

input="$(cat 2>/dev/null || true)"
# Loop guard: if this stop attempt was itself forced by a Stop hook, let it
# through rather than blocking forever on a condition the model cannot clear.
if printf '%s' "$input" | jq -e '.stop_hook_active == true' >/dev/null 2>&1; then
  exit 0
fi

review=$(find .tigerteam/board/review -maxdepth 1 -name 'T-*.md' -mmin "+$GRACE_MIN" 2>/dev/null | wc -l)
blocked=$(find .tigerteam/board/blocked -maxdepth 1 -name 'T-*.md' -mmin "+$GRACE_MIN" 2>/dev/null | wc -l)

sup=0
if [ -f .tigerteam/supervisor.pid ] && kill -0 "$(cat .tigerteam/supervisor.pid 2>/dev/null)" 2>/dev/null; then
  sup=1
fi

# Board-scoped waiter: only count pids whose cwd is this board's ROOT.
# /proc unavailable (non-Linux): treat waiter as armed (fail-open: a guard
# that cannot verify must never nag on every turn).
# Push mode: skip the scan entirely (no pgrep) — the supervisor writes
# digests and there is nothing to re-arm.
waiters=0
if [ "$push" != "true" ]; then
  if [ ! -e /proc/self/cwd ]; then
    waiters=1
  else
    for pid in $(pgrep -f 'tigerteam events --wait' 2>/dev/null || true); do
      cwd=$(readlink "/proc/$pid/cwd" 2>/dev/null || true)
      if [ "$cwd" = "$ROOT" ]; then
        waiters=$((waiters + 1))
      fi
    done
  fi
fi

msgs=""
[ "$review" -gt 0 ] && msgs="$msgs $review ticket(s) waiting in review/ for >${GRACE_MIN}m;"
[ "$blocked" -gt 0 ] && msgs="$msgs $blocked ticket(s) waiting in blocked/ for >${GRACE_MIN}m;"
if [ "$push" != "true" ] && [ "$sup" -eq 1 ] && [ "$waiters" -eq 0 ]; then
  msgs="$msgs supervisor is running but no 'tigerteam events --wait' is armed;"
fi

[ -z "$msgs" ] && exit 0

if [ "$push" = "true" ]; then
  reason="tigerteam PM guard:${msgs} handle blocked/ then review/ (oldest first); digests are in .tigerteam/digests/ (tigerteam events --latest). If you are mid-conversation with the user, deal with the board state first, then answer."
else
  reason="tigerteam PM guard:${msgs} handle blocked/ then review/ (oldest first) and re-arm 'tigerteam events --wait' as a background task before going idle. If you are mid-conversation with the user, deal with the board state first, then answer."
fi
printf '{"decision":"block","reason":%s}\n' "$(printf '%s' "$reason" | jq -Rs .)"
exit 0
