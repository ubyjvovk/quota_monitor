#!/usr/bin/env bash
#
# Cut and push a release tag using CalVer YYYY.M.MICRO from VERSION. The tag
# feeds .github/workflows/release.yml, which strips the leading v and uses the
# rest verbatim.
#
#   ./scripts/release.sh            cut + push the next release
#   ./scripts/release.sh --dry-run  print what the tag would be, do nothing
#   ./scripts/release.sh --next     suggest this month's next CalVer, do nothing

set -euo pipefail

if [ "${1:-}" = "--next" ]; then
  YEAR="$(date +%Y)"
  MONTH="$(date +%m)"
  MONTH="${MONTH#0}"
  HIGHEST=0
  while IFS= read -r existing_tag; do
    micro="${existing_tag#v${YEAR}.${MONTH}.}"
    [[ "$micro" =~ ^[0-9]+$ ]] || continue
    micro=$((10#$micro))
    if (( micro > HIGHEST )); then
      HIGHEST="$micro"
    fi
  done < <(git tag --list "v${YEAR}.${MONTH}.*")
  echo "${YEAR}.${MONTH}.$((HIGHEST + 1))"
  exit 0
fi

TAG="v$(cat VERSION)"

# --dry-run prints the would-be tag only; it is allowed from any branch so
# agents can preview it, but it still refuses a dirty tree (a real dry run
# should reflect only a currently releasable state).
if [ "${1:-}" = "--dry-run" ]; then
  if [ -n "$(git status --porcelain --untracked-files=no)" ]; then
    echo "error: uncommitted tracked changes present; commit or stash before cutting a release" >&2
    exit 1
  fi
  echo "$TAG"
  exit 0
fi

# Releases are cut from a clean master and never re-tagged.
[ "$(git rev-parse --abbrev-ref HEAD)" = "master" ] || {
  echo "error: releases are cut from master (on $(git rev-parse --abbrev-ref HEAD))" >&2
  exit 1; }
[ -z "$(git status --porcelain --untracked-files=no)" ] || {
  echo "error: uncommitted tracked changes present; commit or stash before cutting a release" >&2
  exit 1; }
git rev-parse "$TAG" >/dev/null 2>&1 && {
  echo "error: tag $TAG already exists; run scripts/set-version.sh <next version> and commit" >&2
  exit 1; }

echo "==> Tagging $TAG"
git tag -a "$TAG" -m "Quota Monitor ${TAG#v}"

echo "==> Pushing master and $TAG"
git push origin master "$TAG"

echo "==> Actions: https://github.com/ubyjvovk/quota_monitor/actions"
