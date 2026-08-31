#!/usr/bin/env bash
# Install or update quotamon from the main repository's GitHub release, then
# bootstrap its config. The panel runs this only when Install is clicked; it is
# also safe to invoke by hand and never runs unattended.

set -euo pipefail

release_base=${QUOTAMON_RELEASE_BASE:-https://github.com/ubyjvovk/quota_monitor/releases/latest/download}
bin_dir=${QUOTAMON_BIN_DIR:-"$HOME/.local/bin"}

# Tests and unusual systems can select an architecture explicitly. Otherwise,
# translate the kernel spelling to the names used by release assets.
if [[ -n ${QUOTAMON_ARCH:-} ]]; then
  arch=$QUOTAMON_ARCH
else
  case $(uname -m) in
    x86_64) arch=amd64 ;;
    aarch64|arm64) arch=arm64 ;;
    *)
      echo "Unsupported architecture: $(uname -m)" >&2
      exit 1
      ;;
  esac
fi

case $arch in
  amd64|arm64) ;;
  *)
    echo "Unsupported architecture: $arch (expected amd64 or arm64)" >&2
    exit 1
    ;;
esac

asset="quotamon-linux-$arch"
tmp_dir=$(mktemp -d)
trap 'rm -rf -- "$tmp_dir"' EXIT

# Download into an isolated directory. Nothing under the install prefix is
# touched until both files arrive and the selected asset verifies successfully.
curl -fsSL "$release_base/$asset" -o "$tmp_dir/$asset"
curl -fsSL "$release_base/SHA256SUMS" -o "$tmp_dir/SHA256SUMS"

# Verify only the exact release filename. A missing checksum is just as unsafe
# as a mismatch, so either condition aborts before an install staging file exists.
checksum_line=$(grep -E "^[[:xdigit:]]{64}[[:space:]]+\\*?$asset$" "$tmp_dir/SHA256SUMS") || {
  echo "SHA256SUMS has no checksum for $asset; quotamon was not installed" >&2
  exit 1
}
(
  cd "$tmp_dir"
  printf '%s\n' "$checksum_line" | shasum -a 256 -c -
)

# Stage on the destination filesystem so the final rename replaces an existing
# binary atomically. The EXIT trap also removes a staging file after any failure.
mkdir -p "$bin_dir"
staged_bin=$(mktemp "$bin_dir/.quotamon.XXXXXX")
trap 'rm -rf -- "$tmp_dir"; rm -f -- "${staged_bin:-}"' EXIT
cp "$tmp_dir/$asset" "$staged_bin"
chmod +x "$staged_bin"
bin="$bin_dir/quotamon"
mv -f "$staged_bin" "$bin"
staged_bin=

# Exit 3 is quotamon's explicit "no config" signal. Other provider failures do
# not invalidate a correctly installed binary and remain visible on refresh.
providers_status=0
"$bin" providers >/dev/null 2>&1 || providers_status=$?
if [[ $providers_status -eq 3 ]]; then
  "$bin" setup --yes
  echo "DeepInfra can be added with: quotamon config set deepinfra --api-key-stdin"
fi

echo "Installed quotamon at $bin"
case :$PATH: in
  *:"$bin_dir":*) echo "$bin_dir is already on PATH" ;;
  *) echo "Add $bin_dir to PATH, or set the plugin's quotamon command to $bin" ;;
esac
