# Quota Monitor

A macOS menu bar app and desktop widget showing remaining quota across your
LLM subscriptions. Ships with **Claude** (Max/Pro) and **ChatGPT** (Plus/Pro via
Codex); other providers are a protocol conformance away.

```
CL 43%  ·  GPT 18%                       ← menu bar

┌──────────────────────────────────────────┐
│ Claude 5h runs out in 1h 13m        ⟳ ⇅ │  ← the finding, asserted
│ updated just now                         │
│                                          │
│ Claude   max                        live │
│  5h        ╱‾‾‾‾●⋯⋯⋯⋯⋯          71%     │  ← usage vs. linear-pace diagonal
│            1.2× pace · out in 1h 13m     │
│  Week      ▁▁▁●⋯⋯⋯⋯⋯⋯⋯          12%     │
│            under pace · resets in 4d 23h │
│ ─────────────────────────────────────────│
│ ChatGPT  plus                     2h ago │
│  Week      ▁▁▁▁▁▁●⋯⋯⋯           18%     │
└──────────────────────────────────────────┘
```

## Quick start

```bash
brew install xcodegen
./scripts/build.sh    # build + launch
```

Claude works out of the box via its usage endpoint. The statusLine mirror below
is an optional offline fallback.

## Where the numbers come from

Neither vendor publishes a subscription-quota API, so each provider has **two**
sources and the app prefers whichever is both fresher and trustworthy.

| Provider | Local (default) | Live (opt-out) |
|---|---|---|
| Claude | `~/.quota-monitor/claude-usage.json`, written by a Claude Code **statusLine** hook | `GET api.anthropic.com/api/oauth/usage`, OAuth token from Keychain `Claude Code-credentials` |
| ChatGPT | newest `~/.codex/sessions/**/*.jsonl` rollout, which Codex updates every turn | `chatgpt.com/backend-api/api/codex/usage`, token from `~/.codex/auth.json` |

**Local** costs nothing, touches no tokens, makes no network call — but is only
as fresh as your last CLI turn. **Live** is current but rides undocumented
endpoints. When live fails the cached number is still shown, labelled with the
reason. Turn live off per-provider in the panel's settings to go fully offline.

The Claude statusLine route is appealing because `rate_limits` is part of Claude
Code's *documented* statusLine payload rather than a private API — but it only
fires in terminal Claude Code. Inside the desktop app there is no status line to
render, so nothing ever calls the hook. Live is therefore the default for Claude.

### Verified status

- Claude live — **working**. This is the default Claude source.
- Claude local — **working**, but note the statusLine hook only fires in
  *terminal* Claude Code. The desktop app renders no status line, so the mirror
  is never written there. Useful as an offline fallback if you use the terminal.
- ChatGPT local — **working**
- ChatGPT live — returns `404`; the exact path is undocumented. Override without
  rebuilding: `QUOTA_MONITOR_CODEX_USAGE_URL=https://…`. Local covers this
  provider well, so the impact is small.

### Why not the claude.ai web endpoint?

`GET claude.ai/api/organizations/{org}/usage` is what the web app itself calls,
but it sits behind a Cloudflare managed challenge. The `cf_clearance` cookie is
bound to the originating browser's TLS fingerprint, IP and User-Agent, so
replaying it from `URLSession` returns `403 Just a moment…`. Tested and confirmed.

Making it work would mean either evading the bot check — which this project does
not do — or hosting a `WKWebView` that loads claude.ai so the real browser engine
satisfies the challenge normally, then reading the endpoint from inside that
session. That is legitimate but heavy, and it puts a browser and a live session
cookie inside a menu bar app — all to fetch numbers `/api/oauth/usage` already
returns. Not worth it.

## The widget

The widget builds, embeds and registers, but shows *"Open Quota Monitor"* until
you give it an App Group — a sandboxed widget has no other way to read the app's
snapshot, and App Groups require a signing team.

To enable it, edit [`Config/Signing.xcconfig`](Config/Signing.xcconfig):

```
DEVELOPMENT_TEAM = YOURTEAMID          # a free Apple ID works
CODE_SIGN_STYLE  = Automatic
QM_ENTITLEMENTS_VARIANT = -AppGroup
```

then `./scripts/build.sh`. The menu bar app is unaffected either way.

## Layout

```
QuotaKit/            multiplatform core (macOS + iOS) — models, sources, engine
  Sources/QuotaKit/
    Models/          QuotaWindow, ProviderSnapshot, QuotaSnapshot
    Providers/       QuotaSource protocol, Claude + Codex, hybrid merge
    Engine/          refresh loop, shared snapshot store
  Sources/quotactl/  diagnostic CLI
App/                 SwiftUI MenuBarExtra app
Widget/              WidgetKit extension (macOS today, iOS next)
scripts/             statusline mirror + installer, build
```

## Troubleshooting

`quotactl` reports each source independently, so a wrong number can be traced to
its origin:

```bash
cd QuotaKit && swift run quotactl          # local only
cd QuotaKit && swift run quotactl --live   # also try the endpoints
```

Run the tests with `cd QuotaKit && swift test`.

## Design notes

**The chart answers "compared to what?"** A bare "43% used" is not actionable:
43% is fine four hours into a five-hour window and alarming ten minutes in. Each
window is drawn as a sparkline across the *whole* window — where the line stops
shows the time remaining — against a dashed diagonal representing perfectly even
consumption. Ink above the diagonal means burning faster than the clock. That
single comparison turns a number into a decision.

Consequently the headline asserts a finding ("Claude 5h runs out in 1h 13m")
rather than labelling an axis, and colour marks only the window that is
overspending. Everything else stays gray.

**Progress bars were removed deliberately.** A bar spends a lot of ink to encode
one number and cannot show history or pace. The sparkline carries the number,
its trajectory and its reference in less space.

**Stale readings are corrected, not trusted.** A local reading of "82% used" is
meaningless once the window has reset, so `currentUsedPercent(asOf:)` returns nil
past `resetsAt`.

**Credentials are addressed by path, never searched for.** The Claude Keychain
item holds `claudeAiOauth` alongside `mcpOAuth`, a map of per-MCP-server
credentials that each carry their own `accessToken`. A recursive key search finds
an arbitrary one of those — dictionary order is not stable — and authenticates as
the wrong service, or with an empty string. That produced a persistent `429` that
looked exactly like rate limiting. `JSONValue.firstValue(forKey:)` stays for
parsing unknown *response* shapes; credentials use explicit paths.

**Absence is never zero.** A reset or missing window renders `—` with "no reading
since this window reset". Returning `0%` there would assert a fresh, empty window
we have no evidence for — the same mistake as showing the stale number, in the
opposite direction.

**Dark mode is designed, not inverted.** The accent desaturates from `#e41a1c` to
`#fc8d62`, because a colour tuned for an off-white ground vibrates against a dark
one.

**Adding a provider** means adding one `HybridProvider` to
`QuotaEngine.makeProviders()`. Everything downstream is driven off the normalised
snapshot, so no UI changes are needed.

### Reviewing the design

The panel renders to PNG without needing a screenshot:

```bash
QUOTA_MONITOR_RENDER=/tmp/panel "build/Build/Products/Debug/Quota Monitor.app/Contents/MacOS/Quota Monitor"
```

Writes `panel-light.png` and `panel-dark.png` from representative data, then
exits. Add `QUOTA_MONITOR_RENDER_REAL=1` to render your actual current state.

## iPhone widget

`QuotaKit` already builds for iOS 17+ and the widget views are plain SwiftUI, so
the remaining work is transport: the phone can't read your Mac's `~/.codex`. The
intended route is the Mac app publishing snapshots to CloudKit (or
`NSUbiquitousKeyValueStore` — the payload is under 1 KB) with the iOS widget as a
read-only consumer, which keeps every credential on the Mac.
