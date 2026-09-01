#!/usr/bin/env bash
# Publish generated omarchy/ history to the standalone plugin repository and
# tag it with the current CalVer, triggering the plugin's release workflow.
# quota_monitor remains the source of truth; plugin PRs belong upstream here.
#
#   --no-tag   push master only, even when the version's tag does not exist yet.
#              For putting an unreleased change in front of a reviewer's eyes
#              (`omarchy plugin update` reads master); a later run without the
#              flag cuts the tag once the change is approved.

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

target=git@github.com:ubyjvovk/quotamon-omarchy.git
target_set=false
dry_run=false
no_tag=false

for arg in "$@"; do
  case $arg in
    --dry-run) dry_run=true ;;
    --no-tag) no_tag=true ;;
    -*) echo "Usage: $0 [--dry-run] [--no-tag] [remote-url]" >&2; exit 2 ;;
    *)
      if [[ $target_set == true ]]; then
        echo "Usage: $0 [--dry-run] [--no-tag] [remote-url]" >&2
        exit 2
      fi
      target=$arg
      target_set=true
      ;;
  esac
done

if [[ $(git branch --show-current) != master ]]; then
  echo "Refusing to publish: current branch is not master" >&2
  exit 1
fi

if [[ -n $(git status --porcelain --untracked-files=no) ]]; then
  echo "Refusing to publish with uncommitted tracked changes" >&2
  exit 1
fi

if ! bash "$ROOT/scripts/check-versions.sh"; then
  echo "Refusing to publish: version drift (run scripts/check-versions.sh)" >&2
  exit 1
fi

VERSION="$(cat "$ROOT/VERSION")"
tag="v$VERSION"

# The plugin must ship the digest of the core it names, and that digest must
# still describe the release it was taken from. From here a stale pin and a
# tampered release look identical, so both refuse — including under --dry-run,
# which is meant to answer "would this publish be sound?".
pin_file="$ROOT/omarchy/quotamon-$VERSION.sha256"
if [[ ! -f $pin_file ]]; then
  echo "Refusing to publish: no digest pin for $VERSION (run scripts/pin-quotamon-digest.sh after the release builds)" >&2
  exit 1
fi

extra_pins=""
for candidate in "$ROOT"/omarchy/quotamon-*.sha256; do
  if [[ $candidate != "$pin_file" ]]; then
    extra_pins+=" ${candidate##*/}"
  fi
done
if [[ -n $extra_pins ]]; then
  echo "Refusing to publish: exactly one digest pin may ship, found extras:$extra_pins" >&2
  exit 1
fi

if ! release_sums=$(curl -fsSL "https://github.com/ubyjvovk/quota_monitor/releases/download/$tag/SHA256SUMS"); then
  echo "Refusing to publish: could not reach release $tag to verify the digest pin" >&2
  exit 1
fi
while read -r digest pinned_asset; do
  if [[ -z $digest ]]; then
    continue
  fi
  if ! printf '%s\n' "$release_sums" | grep -qE "^$digest[[:space:]]+\\*?$pinned_asset\$"; then
    echo "Refusing to publish: pinned digest for $pinned_asset does not match release $tag" >&2
    exit 1
  fi
done < "$pin_file"

split_sha=$(git subtree split --prefix=omarchy HEAD)

tag_state=unknown
if remote_tags=$(GIT_TERMINAL_PROMPT=0 git ls-remote --tags "$target" "refs/tags/$tag" 2>/dev/null); then
  if [[ -n $remote_tags ]]; then
    tag_state=present
  else
    tag_state=absent
  fi
fi

echo "Omarchy subtree: $split_sha"
echo "Publish target: $target (master)"
echo "Plugin version: $tag"
echo "Digest pin: quotamon-$VERSION.sha256 verified against release $tag"
case $tag_state in
  absent)
    if [[ $no_tag == true ]]; then
      echo "Tag $tag: skipped (--no-tag) — master updated only; rerun without --no-tag to cut it"
    else
      echo "Tag $tag: will be created"
    fi ;;
  present) echo "Tag $tag: already published — master updated only; bump with scripts/set-version.sh to cut a new plugin release" ;;
  unknown) echo "Tag $tag: unknown (could not reach $target)" ;;
esac

if [[ $dry_run == true ]]; then
  echo "Dry run: nothing pushed"
  exit 0
fi

# This history is generated from quota_monitor; the plugin repo is only a
# publish target, and contributions should be made against the upstream tree.
git push "$target" "$split_sha:refs/heads/master" --force
if [[ $tag_state == absent && $no_tag == false ]]; then
  git push "$target" "$split_sha:refs/tags/$tag"
fi
