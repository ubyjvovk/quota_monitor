#!/usr/bin/env bash
# Publish generated omarchy/ history to the standalone plugin repository and
# tag it with the current CalVer, triggering the plugin's release workflow.
# quota_monitor remains the source of truth; plugin PRs belong upstream here.

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

target=git@github.com:ubyjvovk/quotamon-omarchy.git
target_set=false
dry_run=false

for arg in "$@"; do
  case $arg in
    --dry-run) dry_run=true ;;
    -*) echo "Usage: $0 [--dry-run] [remote-url]" >&2; exit 2 ;;
    *)
      if [[ $target_set == true ]]; then
        echo "Usage: $0 [--dry-run] [remote-url]" >&2
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

tag="v$(cat "$ROOT/VERSION")"
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
case $tag_state in
  absent) echo "Tag $tag: will be created" ;;
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
if [[ $tag_state == absent ]]; then
  git push "$target" "$split_sha:refs/tags/$tag"
fi
