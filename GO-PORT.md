# Go core port — decision record and implementation brief

Decided 2026-08-29. Status: **phase 1 done, phase 2 in progress** (see
"Status" at the end). This document is the design record; read
[PROVIDERS.md](PROVIDERS.md) first — it is the provider contract and every
rule in it applies to the port. For how to *use* the result see
[core/README.md](core/README.md); for how to *work on* it see
[AGENTS.md](AGENTS.md).

## The question

Quota Monitor is outgrowing macOS: an Omarchy (Arch + Hyprland + Waybar)
plugin is planned, and a Windows tray widget is plausible after that. The
core — credential → fetch → normalise → snapshot — is currently Swift
(`QuotaKit`). Where should it live so every platform shares one
implementation? Candidates considered: Python, Go, Swift-everywhere, Rust.

## Decision: Go

One static binary, working name **`quotamon`**, owns everything up to the
normalised snapshot. Every frontend — SwiftUI menu bar app, WidgetKit
widget, Waybar module, future Windows tray — is a dumb renderer of that
snapshot. Nothing above the snapshot fetches; nothing below it renders.

Why Go and not:

- **Python** — fails the deployment constraint that motivated the question.
  macOS ships no Python (the `python3` stub prompts for Command Line
  Tools); Windows ships none. The workaround, PyInstaller/Nuitka bundles,
  means 20–40 MB artifacts, slow cold starts, antivirus false-positives on
  Windows and notarisation friction on macOS. Python's one strong platform
  here (Arch) is the platform where language matters least — Waybar execs
  anything that prints JSON.
- **Swift everywhere** — Swift on Linux is real but the toolchain is heavy,
  `FoundationNetworking` has known potholes, and static distribution is
  awkward; Swift on Windows is frontier. Not worth it for HTTP + JSON +
  subprocess plumbing.
- **Rust** — would work, buys nothing here except slower iteration. There
  is no perf requirement; the workload is four HTTP calls and some JSON.

What Go buys, concretely: single static binary per OS/arch; the whole
workload is stdlib (`net/http`, `encoding/json`, `os/exec`) so **no cgo**,
which keeps cross-compilation a one-liner (`GOOS=linux GOARCH=amd64 go
build` from the Mac, full matrix in one make target); millisecond cold
start, pleasant for a Waybar interval module.

## Architecture

```
quotamon snapshot        normalised snapshot JSON on stdout
quotamon waybar          Waybar custom-module JSON on stdout
quotamon check           per-source diagnostics (quotactl's job today)
```

One-shot CLI, not a daemon. State that must survive invocations (usage
history for pace/sparklines) lives in `~/.quota-monitor/` (honour
`QUOTA_MONITOR_DIR`, already documented in PROVIDERS.md). A daemon adds
nothing until push updates are needed.

Suggested layout: a `core/` Go module at repo root —
`core/cmd/quotamon/`, `core/internal/providers/`, `core/internal/snapshot/`.
Adjust freely; the layout is not the contract.

Environment variables to honour, all pre-existing: `QUOTA_MONITOR_DIR`,
`QUOTA_MONITOR_CODEX_USAGE_URL`, `DEEPINFRA_KEY`.

## The contract: snapshot JSON

The normative schema is the Swift `Codable` models — read them, they are
short and heavily commented:

- `QuotaKit/Sources/QuotaKit/Models/QuotaSnapshot.swift` —
  `{providers: [...], generatedAt}`; encoded ISO-8601 dates, sorted keys,
  written atomically (`SnapshotStore.swift`).
- `Models/ProviderSnapshot.swift` — `id`, `displayName`, `plan?`,
  `windows[]`, `credits?` (`hasCredits`, `unlimited`, `balance?`,
  `enabled` — **`enabled` is load-bearing**, see PROVIDERS.md on Claude's
  `spend`), `observedAt` (when the numbers were *true*, not when read),
  `origin` (`live` | `local` | `unavailable`), `status`.
- `Models/QuotaWindow.swift` — `id`, `label`, `kind`
  (`session|weekly|monthly|other`), `usedPercent`, `resetsAt?`,
  `windowMinutes?`. Semantics that must survive the port: a window past
  `resetsAt` has **no current reading** (nil/dash, never 0%); kind and
  label are *inferred from `windowMinutes`*, never assumed
  (`primary != 5h` by fiat).

**One known wrinkle:** `ProviderStatus` is a Swift enum with associated
values (`ok`, `needsSetup(String)`, `failed(String)`); Swift's synthesized
encoding for that (`{"needsSetup":{"_0":"…"}}`) is hostile as a
cross-language contract. Resolve it deliberately: either replicate the
Swift encoding exactly (capture a fixture of a real `snapshot.json` first
and match it), or — better — define a v2 shape like
`{"state":"needsSetup","message":"…"}` and extend the Swift decoder to
accept both. `QuotaSnapshot.decode` is documented as deliberately
forgiving, so the second path is in-spirit. Do not leave this to chance;
it is the one place the two languages will silently disagree.

Add a golden `snapshot.json` fixture once the shape is settled, and test
that QuotaKit decodes what quotamon emits.

## What ports, what stays, what lands in Go first

Ports (Swift → Go, semantics only, ~600 lines):

- `Providers/ClaudeSources.swift` — Keychain via `security` CLI, the
  `limits`-array preference, codenamed-bucket exclusion, `spend.enabled`.
- `Providers/CodexSources.swift` — `codex app-server --stdio` JSON-RPC
  (close stdin after writing; select reply by `id`), rollout-tail local
  fallback, `wham/usage` HTTP alternative.
- The hybrid merge rule (prefer fresher-and-trustworthy; label stale) from
  `Providers/QuotaSource.swift`, and eventually pace/history
  (`Engine/UsageHistory.swift`) so every platform computes identical
  sparklines. History can be a later phase; snapshot first.

Lands in Go **first, never in Swift** (researched in PROVIDERS.md, not yet
implemented anywhere): **Grok** (working endpoint), **DeepInfra** (working;
spend-not-quota — no invented percentages), **Kimi** (blocked, no endpoint
found — do not implement, do not call `/me`).

Stays Swift: everything that renders — App/, Widget/, `UI/`, the model
types as the app's in-memory representation. The Swift *fetchers* retire
only at the cutover (below).

## Platform landings

**Omarchy / Waybar.** The plugin is the binary plus a config snippet:

```json
"custom/quota": { "exec": "quotamon waybar", "return-type": "json",
                  "interval": 300 }
```

`quotamon waybar` emits `{"text":"CL 43% · GPT 18%","tooltip":"…",
"class":"warning","percentage":43}` — text mirrors the menu bar format,
tooltip carries the per-window detail, class thresholds drive Waybar CSS.
Distribution: static binary via AUR (`quotamon-bin`) or a curl install
script.

**macOS.** The app is deliberately unsandboxed and already shells out to
`security` and `codex app-server`, so exec'ing a bundled Go helper is no
new capability. Integration: app bundles `quotamon` in
`Contents/MacOS/`, runs it on the refresh interval, ingests the snapshot
JSON into the existing `SnapshotStore` path (the widget continues to read
the store; it never execs anything — sandboxed widget extensions can't).

**Windows.** Same core, `GOOS=windows`. The tray UI can itself be Go
(systray libs) importing the core as a package rather than exec'ing it.
Out of scope until the core is proven on the first two platforms.

**iOS.** Unaffected. The README's plan (Mac publishes snapshots via
CloudKit; phone widget is a read-only consumer) has the phone never
fetching, so Go-on-iOS never comes up. The snapshot-as-contract design is
what keeps this true — protect it.

## Credentials are per-OS regardless of language

This work exists in every scenario; it is not a cost of switching. Small
per-`runtime.GOOS` resolver:

| Provider | macOS | Linux | Windows |
|---|---|---|---|
| Claude | `security find-generic-password -s "Claude Code-credentials" -w` (CLI, never the framework API — see PROVIDERS.md) | plain file, believed `~/.claude/.credentials.json` — **verify before relying on it** | **unverified** — check when the port reaches Windows |
| Codex | `codex app-server` / `~/.codex/auth.json` — portable as-is | same | same |
| Grok | `~/.grok/auth.json` — portable | same | same (path separator aside) |
| DeepInfra | `DEEPINFRA_KEY` env — portable | same | same |

Same hazard everywhere: **address tokens by explicit path, never search a
credential blob recursively** (PROVIDERS.md rule 3; the bug shipped once).

## Conformance and cutover

The fixtures under `QuotaKit/Tests/QuotaKitTests/Fixtures/` are plain JSON
captures and are the shared conformance suite: **same fixture in, same
normalised window/credits values out**, asserted in both test suites for
as long as both implementations exist. These endpoints are undocumented
and will drift; fix-it-twice is the real tax of the transition, so keep
the window short:

1. Go core reaches parity on Claude + Codex against fixtures + live.
2. Swift fetchers frozen (bug-fix only, no new providers — new providers
   are Go-only from day one).
3. Mac app consumes the bundled helper; Swift fetcher code deleted.

## Phase 1 scope (the implementation ticket)

`core/` Go module; `quotamon snapshot` covering Claude (live + statusline
mirror) and Codex (app-server + rollout tail), hybrid merge, snapshot JSON
that QuotaKit's decoder accepts; `quotamon waybar`; fixture-driven tests
reusing the existing JSON fixtures; `GOOS` matrix build target for
darwin/arm64, linux/amd64, linux/arm64.

Acceptance: on this Mac, `quotamon snapshot` and `swift run quotactl`
agree on every window percentage; `cd QuotaKit && swift test` still
passes; the linux/amd64 binary cross-compiles from macOS.

Explicitly out of phase 1: Grok/DeepInfra sources (phase 2, spec already
in PROVIDERS.md), pace/history ownership, mac app integration, Windows,
any daemon mode.

## Guardrails (inherited, non-negotiable)

PROVIDERS.md "cross-cutting rules learned the hard way" apply verbatim to
the port. The ones most likely to be violated by a fresh implementation:
no reading is never `0%`; timestamps coerced defensively (ms vs s vs
ISO-8601); explicit credential paths; no PII ingestion (emails, phone
numbers, Stripe IDs); actionable failure messages ("run `grok login`");
prefer vendor CLIs over private HTTP; identify honestly — no spoofed
User-Agents, no cookies, no bot-protection workarounds.

## Status (kept current by the PM)

| Step | State | Evidence |
|---|---|---|
| Phase 1: `core/` module, snapshot contract, Claude + Codex, hybrid merge, `snapshot`/`waybar`/`check`, cross-compile | **done** 2026-08-29 (T-0008–T-0011, T-0014) | `quotamon snapshot` and `quotactl --json` agree on every percentage on the reference Mac; 62 Swift + 9 Go packages green; `make matrix` builds darwin/arm64, linux/amd64, linux/arm64 |
| Status JSON wrinkle | resolved: v2 `{"state","message"}`; Swift decodes both, emits v2 | `snapshot-v2.json` golden fixture, tested from both languages |
| ChatGPT live via `codex app-server` | **working** (0.9 s) — stdin must stay open until the `id: 2` reply; PROVIDERS.md corrected | T-0014 |
| Phase 2: Grok, DeepInfra | **done** (T-0012, T-0013, T-0017) | all four providers live in one `quotamon` call on the reference Mac |
| Bare `quotamon` human table, `--json` alias | **done** (T-0015, T-0020) | — |
| Config: mandatory JSON at `~/.config/quotamon/config.json` (or `$QUOTA_MONITOR_DIR`), keys allowed with 0600 enforced, exit 3 + hint when missing | **done** (T-0018) | — |
| `quotamon setup` (local-only discovery → per-provider Y/n → key prompt → manual add) and `quotamon providers` | **done** (T-0019) | — |
| User-facing docs rewrite | in progress (T-0016) | — |
| Cutover step 2: Swift fetchers frozen | in effect (AGENTS.md) | — |
| Cutover step 3: Mac app bundles `quotamon`, Swift fetchers deleted | **not started** — `App/` is on hold pending the UI rework | — |
| Windows, daemon mode, history/pace in Go | not started (by design) | — |

Corrections to the original text: Go was *not* installed on the reference
Mac (now Go 1.27 via Homebrew); `~/.claude/.credentials.json` **does** exist
on macOS too (T-0009 report), which makes the Linux path more plausible but it
is still unverified on Linux.
