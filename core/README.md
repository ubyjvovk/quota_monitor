# QuotaMon core

QuotaMon is the portable Go core for QuotaKit. It normalises provider usage
into one snapshot contract that command-line and graphical frontends can
decode without knowing provider-specific response formats. The module uses the
Go standard library only and does not require cgo.

## Layout

- `cmd/quotamon/` contains the command-line entry point.
- `internal/hybrid/` resolves live and local attempts into one provider reading.
- `internal/registry/` constructs providers in stable display order.
- `internal/snapshot/` defines the normalised model and JSON contract.
- `internal/source/` defines provider sources and prioritised errors.
- `internal/format/` contains compact shared display formatting.
- `internal/jsonx/` contains explicit-path helpers for loose provider JSON.
- `internal/fixtures/` loads the conformance fixtures shared with QuotaKit.

## Usage

Run the commands from `core/`, or install the built `quotamon` binary on your
`PATH`:

```sh
quotamon                    # human-readable table (this is the default)
quotamon --json             # alias for `snapshot`
quotamon snapshot           # normalized snapshot as JSON
quotamon waybar             # one line of Waybar custom-module JSON
quotamon check              # probe every source independently
quotamon setup              # configure providers interactively
quotamon providers          # list providers and whether each is enabled
```

The bare invocation fetches every provider and prints the human-readable table
in registry order:

```text
Claude       max            live · just now
  Fave wk   █████░░░░░░░░░░░░░░░  23%  1d 16h
  Week      ███░░░░░░░░░░░░░░░░░  15%  1d 16h
  5h        █░░░░░░░░░░░░░░░░░░░   6%  2h 39m
  credits   20.00 (not enabled)

ChatGPT      plus           live · just now
  5h        ████████████████████ 100%  8m
  Week      ██████░░░░░░░░░░░░░░  31%  5d 11h

Grok         —              live · just now
  Week      █████████████░░░░░░░  63%  2d 13h

DeepInfra    pay-as-you-go  live · just now
  spend     $7.96 this month
```

Table colour defaults to `--color=auto`, which colours warning and critical
usage only when standard output is a terminal. Use `--color=always` or
`--color=never` to override terminal detection; setting `NO_COLOR` to any value
always disables colour, including when `--color=always` is present.

`snapshot` fetches all providers concurrently, prefers usable live readings,
and writes the normalised snapshot JSON; `--json` is an alias for it. A failed
live refresh falls back to a current local reading and labels it as cached.
`waybar` performs the same resolved fetch and writes one line of custom-module
JSON. `check` bypasses the hybrid merge, probes every enabled source
independently, and reports its window count and plan or its actionable error
kind. All commands cap the overall operation at ten seconds. `--no-live`
guarantees that live endpoints, credentials, and subprocesses are skipped.

## Exit codes

- `0` when the table or `snapshot` finds at least one provider window, when
  `waybar` or `check` writes its output, or when help is requested;
- `1` when the table or `snapshot` finds no provider windows, or when output
  encoding fails;
- `2` for an unknown command, flag, or extra argument;
- `3` when the mandatory config file is absent or unreadable (except `waybar`,
  which renders a run-setup payload and exits `0`).

## Configuration

QuotaMon reads a single mandatory JSON config file that every frontend shares.
The path is, in order of precedence:

1. `$QUOTA_MONITOR_DIR/config.json` when that variable is set;
2. `$XDG_CONFIG_HOME/quotamon/config.json`;
3. `~/.config/quotamon/config.json` (on every operating system).

There is no default-on configuration: without the file every command except
`--help`, `setup`, and `providers` tells you to run `quotamon setup` (and the
`waybar` module renders a run-setup message instead of failing).

The file is versioned and lists each provider with its settings and an `enabled`
flag:

```json
{
  "version": 1,
  "providers": {
    "claude": { "enabled": true },
    "codex": { "enabled": true, "live": "app-server" },
    "grok": { "enabled": true },
    "deepinfra": { "enabled": true, "api_key": "your-key" },
    "kimi": { "enabled": false }
  }
}
```

`codex.live` selects its quota route: `"app-server"` (default), `"http"`, or
`"off"`. `deepinfra.api_key` supplies the DeepInfra key directly; when it is
empty the provider falls back to the `DEEPINFRA_KEY` environment variable.

API keys may live in the file, but the config is always written with `0600`
permissions, and a config that contains an `api_key` while carrying group or
other read bits is refused with a message telling you to run `chmod 600 <path>`.
Every command before `setup` demands the file be private; `setup` is how you
create it.

A missing config exits `3` with `No config yet — run: quotamon setup` on
stderr. `waybar` exits `0` and prints
`{"text":"quota: run setup","tooltip":"Run `quotamon setup` in a terminal","class":"unavailable","percentage":0}`
so the module shows a friendly message instead of disappearing.

### First run

Run `quotamon setup` once to create the config. It probes local credentials
to see which providers are ready, shows the findings in one screen, and asks
which to enable:

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
Add anything else manually? [y/N]
Wrote /Users/you/.config/quotamon/config.json (mode 0600). Run: quotamon
```

The default answer for each prompt is Y when the provider was found (or when
it is already enabled on disk) and n otherwise. A provider that needs an API
key and is enabled without one prompts for it; an empty answer leaves it
disabled. `Add anything else manually?` enables a known provider that
discovery missed (for example codex on an unusual path); there is nothing to
add beyond the known set, so the prompt accepts only a known id and the
wizard says so. Setup is re-runnable: it starts from the existing file, so
pasted keys and manual choices survive.

`quotamon setup --yes` performs the same discovery but skips every prompt: it
enables every found supported provider (even one whose config entry was
explicitly off before it was supported), keeps whatever is already enabled on
disk, and takes keys from the environment only — DeepInfra's key is the
`DEEPINFRA_KEY` variable — so it is safe to run non-interactively. To leave a
found provider off, run without `--yes` or edit the config file.
`quotamon providers` prints
the same table plus an `on`/`off` column read from the config, and exits `3`
with the setup hint when the config file does not exist yet.

## Environment variables

QuotaMon honours a small set of environment variables, all optional:

- `QUOTA_MONITOR_DIR` — base directory for QuotaMon state. When set, it wins
  the config path (`$QUOTA_MONITOR_DIR/config.json`) and the Claude statusline
  mirror path (default `~/.quota-monitor/claude-usage.json`).
- `QUOTA_MONITOR_CODEX_USAGE_URL` — turns on the optional HTTP route for
  ChatGPT (`wham/usage`) instead of the default `codex app-server`; reads the
  token from `~/.codex/auth.json`.
- `DEEPINFRA_KEY` — DeepInfra API key, used when the config file's
  `deepinfra.api_key` is empty.

## Caching and refresh

QuotaMon saves each usable live reading as a private JSON file in the `cache/`
directory beside `config.json` (normally `$XDG_CONFIG_HOME/quotamon/cache`, or
`~/.config/quotamon/cache` when `XDG_CONFIG_HOME` is unset). When a provider's
credential is stale by its own timestamp, QuotaMon reuses that reading without
network access only while it is younger than the provider's shortest quota
window and at least one recorded window has not reset. Pass `--fresh` to the
default command, `snapshot`, or `waybar` to bypass this cache. For Kimi, a
needed refresh briefly launches the Kimi CLI in a pseudo-terminal so Kimi can
rotate and save its own credentials; QuotaMon never rotates Kimi tokens itself
or calls the token endpoint directly.

## Waybar

Add the custom module to the Waybar configuration:

```json
"custom/quota": { "exec": "quotamon waybar", "return-type": "json",
                  "interval": 300 }
```

The payload has `text`, `tooltip`, `class`, and `percentage` keys. The headline
uses the tightest current window for each provider, for example
`CL 43% · GPT 18%`. Its class is `normal` below 70%, `warning` below 90%, and
`critical` otherwise; it is `unavailable` when no provider has a current
reading. The tooltip includes every provider window, reset countdown, source,
observation age, and non-OK status.

## Providers

Supported providers are Claude, ChatGPT / Codex, Grok, DeepInfra, and Kimi.

### Claude

The live source reads Claude Code's OAuth credential JSON. On macOS it runs
`/usr/bin/security find-generic-password -s "Claude Code-credentials" -w`;
on Linux, Windows, and other platforms it reads
`~/.claude/.credentials.json`. Credential fields are addressed only through
`claudeAiOauth` (with a legacy top-level fallback), because the same blob can
contain unrelated MCP access tokens.

Live usage comes from `GET https://api.anthropic.com/api/oauth/usage` using the
Claude OAuth token. The parser prefers the response's self-describing `limits`
array, including scoped weekly limits absent from legacy top-level fields.
Only session, `weekly_all`, and `weekly_scoped` entries are exposed, so
codenamed experimental buckets are not mistaken for user quota. If no usable
`limits` array exists, the parser falls back to the statusline mirror's direct
`five_hour` and `seven_day` nodes.

The local source reads `claude-usage.json` from `QUOTA_MONITOR_DIR`, or from
`~/.quota-monitor` when the override is unset. It needs no credential or
network access.

### ChatGPT / Codex

The provider has three quota routes. The default live route runs
`codex app-server --stdio` and requests `account/rateLimits/read`; bearer
tokens remain inside the vendor CLI. The local fallback reads the last
`rate_limits` record from the tails of the newest
`~/.codex/sessions/**/*.jsonl` rollouts. Setting
`QUOTA_MONITOR_CODEX_USAGE_URL` selects the optional HTTP route, which reads
the token from `~/.codex/auth.json`.

App-server output is newline-delimited JSON-RPC mixed with initialization
replies and notifications. The parser selects only reply `id: 2`, then reads
`result.rateLimits` by explicit path. It never searches recursively, because
the sibling `rateLimitsByLimitId` subtree can contain repeated decoy fields.

All three routes share one normalizer. It accepts snake-case and camel-case
window spellings, infers labels and kinds from reported durations, and
discards endpoint identity fields. HTTP requests identify themselves as
`quotamon`; they do not send an `originator` header.

### Grok

The live source reads the Grok CLI bearer token from `~/.grok/auth.json`. The
file is keyed by OIDC scope, so QuotaMon deterministically selects the first
key beginning with `https://auth.x.ai::` and reads only that object's `key`
and `expires_at` fields. It does not ingest the neighboring email or profile
data.

Usage comes from
`GET https://cli-chat-proxy.grok.com/v1/billing?format=credits` with the
`x-grok-client-mode: grok-build` header. The response's `productUsage` entries
are a breakdown of one shared allowance, not independent quotas. QuotaMon
therefore exposes exactly one weekly window from `config.creditUsagePercent`
and never turns product breakdowns into additional windows.

### DeepInfra

The live source reads the **`DEEPINFRA_KEY`** environment variable and sums
month-to-date spend from `api.deepinfra.com/payment/{config,usage}`. DeepInfra
is pay-as-you-go, so a `monthly_spend` percentage window appears only when the
account has a positive spending limit; otherwise it reports spend with no
percentage.

### Kimi

The live source reads the Kimi CLI bearer token from
`~/.kimi-code/credentials/kimi-code.json` (top-level `access_token` only — the
blob also carries a refresh token and profile data). The token lives only 15
minutes and the TUI refreshes it on launch. QuotaMon normally keeps using a
current cached reading while that token is stale; once a refresh is needed it
briefly launches the TUI and then reads `/usages` with the newly persisted
token.

Usage comes from `GET https://api.kimi.com/coding/v1/usages` — note the
**plural**; the singular route is 404. The `usage` object is the weekly pool
(Kimi reports the numbers as strings), and each `limits[]` entry becomes an
additional window whose label and kind are inferred from its duration. A zero
or unparsable limit is skipped rather than fabricated, and the plan comes from
`membership.level` with the `LEVEL_` prefix stripped.

## Build and test

Run `make build` to create the host binary at `bin/quotamon`. Run `make test`
to execute `go vet ./...` followed by `go test ./...`. Run `make matrix` to
cross-compile Darwin arm64, Linux amd64, and Linux arm64 binaries with cgo
disabled.

Provider status uses the v2 object shape: `{"state":"ok"}` or a
`needsSetup`/`failed` state with a `message`. QuotaKit emits this form and still
accepts its legacy synthesized enum representation so persisted snapshots
remain readable.

All timestamps emitted in snapshot JSON are RFC 3339 UTC with no fractional
seconds. Decoding remains permissive and accepts RFC 3339 fractional seconds
and numeric offsets, but the no-fraction output rule is required because
Swift's ISO-8601 snapshot decoder rejects fractional seconds.
