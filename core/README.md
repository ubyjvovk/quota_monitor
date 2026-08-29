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
quotamon snapshot [--no-live]
quotamon waybar [--no-live]
quotamon check [--no-live]
```

`snapshot` fetches all providers concurrently, prefers usable live readings,
and writes the normalised snapshot JSON. A failed live refresh falls back to a
current local reading and labels it as cached. `waybar` performs the same
resolved fetch and writes one line of custom-module JSON. `check` bypasses the
hybrid merge, probes every enabled source independently, and reports its
window count and plan or its actionable error kind. All commands cap the
overall operation at ten seconds. `--no-live` guarantees that live endpoints,
credentials, and subprocesses are skipped.

Exit codes are:

- `0` when `snapshot` finds at least one provider window, when `waybar` or
  `check` writes its output, or when help is requested;
- `1` when `snapshot` finds no provider windows or output encoding fails;
- `2` for an unknown command, flag, or extra argument.

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

Supported providers are Claude, ChatGPT / Codex, and Grok.

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
