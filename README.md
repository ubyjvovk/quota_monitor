# Quota Monitor

Quota Monitor shows how much of your LLM subscription quota you have left, as a
small portable command-line tool called **`quotamon`** plus a Waybar module
for tiling-window Linux desktops. It reads your **Claude** (Anthropic),
**ChatGPT** (Codex), **Grok** (xAI) and **DeepInfra** accounts and prints one
normalised table so you can see, at a glance, which provider you are about to
hit your ceiling on. Kimi is recognised but exposes no quota endpoint yet, so
it is not shown. A macOS menu-bar app and widget are planned (currently on
hold — the new design is SwiftUI over the same core).

The heavy lifting lives in [`core/`](core/README.md), a single static Go
binary. Everything above the normalised snapshot — the macOS app, the widget,
the Waybar module — is a dumb renderer of it.

## Quick start

Build the binary, then let it discover your accounts:

```bash
cd core && make build
./bin/quotamon setup      # first run: discovers accounts, writes config
```

`setup` probes for local credentials and shows what it found before asking
which providers to enable:

```text
Looking for providers…
✓ Claude      Keychain item present
✓ ChatGPT     ~/.codex/auth.json
✗ Grok        not signed in — run `grok login`
✗ DeepInfra   no API key
· Kimi        credentials found, but Kimi exposes no quota API yet

Enable Claude? [Y/n]
Enable ChatGPT? [Y/n]
Enable Grok? [y/N]
Enable DeepInfra? [y/N]
DeepInfra API key (input hidden is not available; paste and press Enter):
Wrote /Users/you/.config/quotamon/config.json (mode 0600). Run: quotamon
```

Then just run it:

```bash
./bin/quotamon
```

```text
Claude          max    live · just now
  5h               43%  resets in 2h 11m
  Week             20%  resets in 4d 23h
  credits        20.00 (not enabled)
ChatGPT         plus   cached · 3m ago
  Week             18%  resets in 6d 7h
Grok            —      live · just now
  Week             63%  resets in 4d 6h
DeepInfra       pay-as-you-go live · just now
  credits       $7.75 this month
```

Configuration lives in one JSON file (see the path precedence in
[`core/README.md`](core/README.md#configuration)). API keys may live in that
file, but the file is always written with mode **0600**, and a config carrying
an `api_key` with looser permissions is refused — fix it with
`chmod 600 <path>`.

The other commands:

```bash
./bin/quotamon --json     # normalized snapshot as JSON (alias for `snapshot`)
./bin/quotamon waybar     # one line of Waybar custom-module JSON
./bin/quotamon check      # probe every source independently
./bin/quotamon providers  # which providers are enabled
```

Cross-compile for all supported platforms in one shot:

```bash
make matrix               # darwin/arm64, linux/amd64, linux/arm64
```

## Omarchy / Waybar

`quotamon` ships a Waybar custom module. Add this to your Waybar config:

```json
"custom/quota": { "exec": "quotamon waybar", "return-type": "json",
                  "interval": 300 }
```

The module emits `text`, `tooltip`, `class`, and `percentage`. Style it with
`#custom-quota` in your Waybar CSS; the class is `normal` below 70%,
`warning` below 90%, `critical` above that, and `unavailable` when no provider
has a current reading, so you can colour each state. `interval` is in seconds
(here, five minutes); `quotamon` runs a fresh fetch on every call, so keep it
coarse enough for your machine.

## Where the numbers come from

No vendor publishes a subscription-quota API, so each provider is read
from wherever its CLI keeps state. Details, endpoints and gotchas for every
provider are in [`PROVIDERS.md`](PROVIDERS.md).

| Provider | Credential | Route | Status |
|---|---|---|---|
| Claude | Claude Code OAuth via the `security` CLI (Keychain) | `api.anthropic.com/api/oauth/usage` | ✅ live |
| ChatGPT / Codex | handled by the vendor CLI, no token touched | `codex app-server` JSON-RPC (`account/rateLimits/read`) | ✅ live |
| Grok | bearer token in `~/.grok/auth.json` | `cli-chat-proxy.grok.com/v1/billing?format=credits` | ✅ live |
| DeepInfra | `DEEPINFRA_KEY` (env or config) | `api.deepinfra.com/payment/*` | ✅ live — spend, not quota |
| Kimi | `~/.kimi-code/credentials/kimi-code.json` | none found | ❌ no quota endpoint |

Each provider has up to **two** sources, and the tool prefers whichever is
fresher *and* trustworthy. The **live** source talks to the account and is
current, but rides undocumented endpoints. The **local** source reads a file a
vendor CLI has already written (Claude's statusline mirror, Codex's session
rollouts); it costs nothing and touches no tokens, but is only as fresh as
your last CLI turn.

That freshness matters: a quota window that has **reset** since it was
recorded shows **`—`**, never `0%`. Returning `0%` there would claim a fresh,
empty window the tool has no evidence for, and would read as "plenty left"
when the truth is "we don't know yet". So Live wins when it succeeds;
otherwise the cached reading is shown *and* labelled with why it is stale.

## Principles

- **Absence is never zero.** A reset or missing window renders `—`, never a
  zero the user could misread as "plenty left".
- **Credentials are addressed by path, never searched for.** Claude's Keychain
  item and Grok's auth file both hold several services' tokens side by side; a
  recursive "find an access token" finds the wrong one. A silent-key bug
  shipped once already, so every credential is taken from an explicit,
  deterministic path.
- **Stale readings are corrected, not trusted.** A recording past its
  `resetsAt` reports no current reading.
- **Identify honestly.** No spoofed User-Agents, no cookies lifted from a
  browser, no bot-protection workarounds. Where a vendor ships a local CLI
  (`security`, `codex app-server`), use it over its private HTTP API.

The sparkline / pace design you may have seen in older versions of this
project belongs to the **macOS app** (currently on hold). It answers
"compared to what?" by drawing each window across its full span against a
diagonal of even consumption — a number becomes a decision. That reasoning
still stands and will return with the app.

## macOS app & widget

**On hold.** The SwiftUI menu-bar app and WidgetKit widget are paused while
the UI is redesigned; the Swift fetchers are frozen at reference behaviour and
nothing new is built there. When they resume, build as before:

```bash
brew install xcodegen
./scripts/build.sh
```

The plan is that the app bundles the `quotamon` binary in its
`Contents/MacOS/`, runs it on the refresh interval, and ingests the snapshot
JSON — the widget keeps reading the shared snapshot store and never execs
anything. See [GO-PORT.md](GO-PORT.md) → "Platform landings" for the detail.

## Layout

```
core/                portable Go core — the snapshot contract and every source
  cmd/quotamon/      the CLI (table, --json/snapshot, waybar, check, setup)
  internal/providers/ per-provider sources (claude, codex, grok, deepinfra)
QuotaKit/            macOS app core — Swift models, sources, engine (frozen)
  Sources/quotactl/  diagnostic CLI (the parity reference)
App/                 SwiftUI menu bar app (ON HOLD)
Widget/              WidgetKit extension (ON HOLD)
scripts/             build + the Claude statusline mirror installer
```

## Troubleshooting

`quotamon check` probes every source independently, so a wrong number can be
traced to its origin. Each line reports a source and an error *kind*:

| Kind | Meaning | What to do |
|---|---|---|
| `notConfigured` | an optional source wasn't set up, or no credential found | enable the source, or sign in (e.g. `claude`, `codex login`, `grok login`) |
| `unauthorized` | the credential was rejected | sign in again; the message says which provider |
| `transport` | a network or endpoint hiccup | try again — a retry may succeed, or the endpoint moved |
| `malformed` | the response couldn't be parsed | probably endpoint drift; an update will address it |
| `noDataFound` | a configured source contained no usable reading | run the vendor CLI once so a local rollout exists |
| `skipped (--no-live)` | a live-only source was not probed | expected under `--no-live` |

Two notes:

- **Claude reads its credential via the `security` CLI, never the framework
  API.** The framework API either refuses an unsigned binary or pops an
  interactive "allow access?" dialog, which a console tool must never block
  on. The `security` CLI returns the item silently.
- **ChatGPT reports usage only after a Codex turn.** If every Codex window has
  reset since your last turn, the tool says so plainly ("…only after a Codex
  turn — last reading 3m ago") rather than implying an outage.

## Contributing / for agents

- [`AGENTS.md`](AGENTS.md) — how to work here (layout, conventions, commands, gotchas).
- [`PROVIDERS.md`](PROVIDERS.md) — the provider contract: endpoints, credentials, dead ends.
- [`GO-PORT.md`](GO-PORT.md) — the design record and status of the Go core port.
