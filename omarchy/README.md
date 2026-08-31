# Omarchy bar plugin

A dumb renderer of `quotamon --json`, in the same spirit as the macOS
menu-bar app and the Waybar module.

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
```

2. Click the Quota Monitor icon to open its panel.
3. Click **Install quotamon**.

The button runs the fetch script included in the plugin. It downloads the
binary for the current architecture, verifies it against the release's
`SHA256SUMS`, installs it in `~/.local/bin`, and runs non-interactive setup when
there is no config yet. It never runs automatically.

To run the same installer manually:

```bash
bash ~/.config/omarchy/plugins/quotamon/fetch-quotamon.sh
```

As a by-hand fallback, download `quotamon-linux-amd64` or
`quotamon-linux-arm64` and `SHA256SUMS` from the main repository's release,
verify the checksum, make the binary executable, and run `quotamon setup`.
`quotamon` must be on `PATH`; if it is not, set the widget's `exec` setting to
the binary's absolute path.

File saves under `~/.config/omarchy/plugins/` hot-reload plugin *code*,
but a bar icon that is already mounted often keeps the old instance.
Remount with `omarchy restart shell`.

On per-monitor bars, each bar instance fetches independently.
