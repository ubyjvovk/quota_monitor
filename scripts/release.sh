#!/usr/bin/env bash
#
# Cut and push a release tag. Versions are {major}.{commit count on master}
# (e.g. v0.264); the tag feeds .github/workflows/release.yml, which strips the
# leading v and uses the rest verbatim.
#
#   ./scripts/release.sh            cut + push the next release
#   ./scripts/release.sh --dry-run  print what the tag would be, do nothing

set -euo pipefail

# MAJOR is bumped by hand on breaking rewrites; the minor is the commit count.
MAJOR=0

COUNTS="$(git rev-list --count HEAD)"
TAG="v${MAJOR}.${COUNTS}"

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
  echo "error: tag $TAG already exists" >&2
  exit 1; }

echo "==> Tagging $TAG"
git tag -a "$TAG" -m "Quota Monitor ${TAG#v}"

echo "==> Pushing master and $TAG"
git push origin master "$TAG"

echo "==> Actions: https://github.com/ubyjvovk/quota_monitor/actions"
