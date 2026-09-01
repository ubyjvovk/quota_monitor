# Omarchy bar plugin

A dumb renderer of `quotamon --json`, in the same spirit as the macOS
menu-bar app and the Waybar module.

## Generated repository

This repository is generated from the `omarchy/` directory in
[`ubyjvovk/quota_monitor`](https://github.com/ubyjvovk/quota_monitor), which is
the source of truth. Open pull requests there; this repository's `master`
branch is force-pushed from the upstream subtree. Each publish tags
`v<CalVer>` to match the main repository's release of the same version. The
installable zip for each tag is available from this plugin repository's
[Releases page](https://github.com/ubyjvovk/quotamon-omarchy/releases).

`preview.png` beside the QML is the plugin's preview image — Omarchy looks for
that exact filename in the plugin directory and no manifest key declares it, so
keep the name if you replace the picture.

The bar icon stacks one thin capsule per provider, filled to that
provider's tightest current window and coloured by severity (`normal`
below 70%, `warning` below 90%, `critical` above that). Click opens a
dropdown with windows, credits, and a Refresh button (`quotamon --json
--fresh`). A fetch that hangs longer than 30 s is hard-killed and reported as a
timeout while the last good snapshot is kept. Bars follow the panel's provider
order (top bar = first row), matching the macOS glyph.

## Install

1. Add the plugin:

```bash
omarchy plugin add https://github.com/ubyjvovk/quotamon-omarchy
omarchy plugin enable ubyjvovk.quotamon
```

2. Click the Quota Monitor icon to open its panel.
3. Click **Install quotamon**.

The button runs the fetch script included in the plugin. It downloads the
binary for the current architecture, verifies its checksum, installs it in
`~/.local/bin`, and runs non-interactive setup when there is no config yet. When
the installed core is older than the plugin, the panel also offers an **Update
quotamon** button. Both buttons install the exact core version shipped with the
plugin rather than whichever release is newest. Nothing is ever installed or
updated automatically; a person must click.

The plugin ships the SHA-256 of the core binaries it was released with, as
`quotamon-<version>.sha256` beside the script, and the Install and Update
buttons verify the download against **that** — not against a `SHA256SUMS`
downloaded next to the binary, which comes from the same release and so is
controlled by whoever controls that release. Swapping a release asset therefore
fails the check unless the plugin repository is changed too, and that leaves a
commit in git history where a person can see it. A version the plugin carries no
digest for is refused rather than guessed: update the plugin so the two versions
match. Both repositories live under one GitHub account, so this raises the bar —
it is not a defence against that account being compromised.

To run the same installer manually:

```bash
bash ~/.config/omarchy/plugins/ubyjvovk.quotamon/fetch-quotamon.sh
```

Passing a version (`... fetch-quotamon.sh 2026.9.2`) pins the download to that
release; the `QUOTAMON_RELEASE_BASE` override applies only when no version is
passed, and setting both is refused.

As a by-hand fallback, download `quotamon-linux-amd64` or
`quotamon-linux-arm64` and `SHA256SUMS` from the main repository's release,
verify the checksum, make the binary executable, and run `quotamon setup`.
`quotamon` must be on `PATH`; if it is not, set the widget's `exec` setting to
the binary's absolute path.

The manifest declares the minimum `quotamon` version and the installer commands
for humans:

```json
"dependencies": {
  "quotamon": ">=2026.9.1",
  "commands": ["curl", "shasum"]
}
```

Omarchy ignores this informational block. The panel enforces the `quotamon`
minimum itself: its footer shows the Quota Monitor plugin version beside the
version reported by `quotamon`, and displays an update warning when that binary
is too old.

File saves under `~/.config/omarchy/plugins/ubyjvovk.quotamon/` hot-reload plugin *code*,
but a bar icon that is already mounted often keeps the old instance.
Remount with `omarchy restart shell`.

On per-monitor bars, each bar instance fetches independently.
