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

`quotamon` must be on `PATH`. If it is not, set the widget's `exec` setting
to the absolute path of the binary. Start from a checkout and copy the plugin
directory into the Omarchy plugin tree:

```bash
git clone https://github.com/ubyjvovk/quota_monitor.git && cd quota_monitor
mkdir -p ~/.config/omarchy/plugins/quotamon
cp -a omarchy/. ~/.config/omarchy/plugins/quotamon/
omarchy restart shell
omarchy plugin enable quotamon --section center --after omarchy.clock
```

Omarchy's `plugin add` expects `manifest.json` at the git repo root, so
this nested copy is the supported install path until the plugin lives in
its own repository.

File saves under `~/.config/omarchy/plugins/` hot-reload plugin *code*,
but a bar icon that is already mounted often keeps the old instance.
Remount with `omarchy restart shell`.

On per-monitor bars, each bar instance fetches independently.
