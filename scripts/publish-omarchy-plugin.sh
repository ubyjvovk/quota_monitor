#!/usr/bin/env bash
# Publish generated omarchy/ history to the standalone plugin repository.
# quota_monitor remains the source of truth; plugin PRs belong upstream here.

set -euo pipefail

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

split_sha=$(git subtree split --prefix=omarchy HEAD)
echo "Omarchy subtree: $split_sha"
echo "Publish target: $target (master)"

if [[ $dry_run == true ]]; then
  echo "Dry run: nothing pushed"
  exit 0
fi

# This history is generated from quota_monitor; the plugin repo is only a
# publish target, and contributions should be made against the upstream tree.
git push "$target" "$split_sha:refs/heads/master" --force
