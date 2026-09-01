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
binary for the current architecture, verifies it against the release's
`SHA256SUMS`, installs it in `~/.local/bin`, and runs non-interactive setup when
there is no config yet. It never runs automatically.

To run the same installer manually:

```bash
bash ~/.config/omarchy/plugins/ubyjvovk.quotamon/fetch-quotamon.sh
```

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
