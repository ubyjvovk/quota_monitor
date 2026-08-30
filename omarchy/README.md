# Omarchy bar plugin

A dumb renderer of `quotamon --json`, in the same spirit as the macOS
menu-bar app and the Waybar module.

The bar icon stacks one thin capsule per provider, filled to that
provider's tightest current window and coloured by severity (`normal`
below 70%, `warning` below 90%, `critical` above that). Click opens a
dropdown with windows, credits, and a Refresh button (`quotamon --json
--fresh`).

## Install

`quotamon` must be on `PATH` (or set the widget's `exec` setting). Then
copy this directory into the Omarchy plugin tree:

```bash
mkdir -p ~/.config/omarchy/plugins/quotamon
cp -a . ~/.config/omarchy/plugins/quotamon/
omarchy restart shell
omarchy plugin enable quotamon --section center --after omarchy.clock
```

Omarchy's `plugin add` expects `manifest.json` at the git repo root, so
this nested copy is the supported install path until the plugin lives in
its own repository.

File saves under `~/.config/omarchy/plugins/` hot-reload plugin *code*,
but a bar icon that is already mounted often keeps the old instance.
Remount with `omarchy restart shell`.
