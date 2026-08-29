# Tiger Team state

## Mission
Make QuotaKit reliably report how much LLM subscription quota is left, **as a
console tool first**. "Done" for this phase: `quotactl` prints every configured
provider's current quota, with Claude's live reading actually working. The
macOS menu-bar app and widget are explicitly ON HOLD — the human is reworking
their UI elsewhere — and workers are told not to touch `App/`, `Widget/`, or
`Sources/QuotaKit/UI/`.

## Configuration notes
- **Host mode + Seatbelt.** No docker on this Mac. `~/.tigerteam.toml` sets
  `image = ""` and `sandbox_profile = ".tigerteam/scripts/worker.sb"`.
  `seatbelt-smoke.sh` passes all hard checks; all installed engines run.
- **`test_cmd = "swift test --disable-sandbox --package-path QuotaKit"`.**
  `--disable-sandbox` is mandatory, not cosmetic: SwiftPM evaluates
  `Package.swift` inside its own nested `sandbox-exec`, and macOS refuses to
  nest Seatbelt sandboxes — without it every worker build dies with
  `sandbox_apply: Operation not permitted`.
- `worker.sb` was extended (2026-08-29) with read+write carve-outs for
  `~/.swiftpm`, `~/Library/org.swift.swiftpm`,
  `~/Library/Caches/org.swift.swiftpm`, `~/Library/Developer`. Without them
  SwiftPM disables all user-level caching and recompiles the manifest every
  run.
- Workers: `codex` and `grok`, both `max_complexity = 2`, host mode.
- Baseline before any ticket: 37 tests, all passing.

## Decision log (append-only)
- 2026-08-29 — Console (`quotactl`) is the near-term deliverable; app/widget on
  hold — the human is doing UI work separately, and console output is
  verifiable by workers while a SwiftUI app is not.
- 2026-08-29 — Read the Claude token **only** via
  `security find-generic-password -s "Claude Code-credentials" -w`, never
  `SecItemCopyMatching` — product owner's call. The framework API is refused
  for an unsigned SwiftPM binary, and when it isn't it raises an interactive
  "allow access?" dialog; a CLI must never block on a GUI prompt. The
  `secItem` strategy stays in the file as an opt-in for a future signed app.
- 2026-08-29 — Parse Claude usage from the response's `limits` array rather
  than hard-coded top-level keys, keeping the old keys as fallback so the
  statusline mirror (older shape) keeps working.

## Verified facts (PM, 2026-08-29 — established by hand, not assumed)
- `security find-generic-password -s "Claude Code-credentials" -w` → exit 0,
  silent, returns the JSON blob. The CLI path works.
- `GET https://api.anthropic.com/api/oauth/usage` with that token + headers
  `anthropic-beta: oauth-2025-04-20`, `anthropic-version: 2023-06-01` → **HTTP
  200**. The endpoint and header set in `ClaudeLiveSource` are correct.
- The response has **no `rate_limits` wrapper**; windows are top-level and use
  `utilization`. It also carries a canonical `limits[]` array.
- **Bug found:** that array holds a `weekly_scoped` entry at **20%**,
  `is_active: true`, which the current parser misses entirely — it reports 14%
  as the worst weekly figure. The tool under-reports quota pressure. This is
  T-0003.
- `seven_day_opus` is now `null`; the hard-coded "Opus wk" window is dead.
- A real response is committed as
  `QuotaKit/Tests/QuotaKitTests/Fixtures/claude-usage-live.json` (checked: no
  tokens, ids, or account data in it) so sandboxed workers can test the true
  shape without network or Keychain.
- **Known issue, not yet ticketed:** the Codex/ChatGPT *live* source returns
  **HTTP 404** — the backend endpoint has moved. Its local rollout source still
  works but was 15 days stale at last check. Needs endpoint research before it
  can be specified as a ticket; deliberately not handed to a C2 worker.

## Board snapshot
- 2026-08-29 — board initialised. T-0001, T-0002, T-0003 drafted and queued to
  `todo/`. Nothing accepted yet.

## Next actions
1. Smoke one worker (`tigerteam worker run codex --once`) and confirm the
   ticket lands in `review/` with a sane report before scaling up.
2. `tigerteam up`, then handle digests: review oldest first.
3. On T-0002 landing, run `quotactl` by hand and eyeball the real output.
4. After T-0001 + T-0003 land, re-verify live Claude end to end and confirm the
   20% scoped weekly window actually appears.
5. Then decide on the Codex 404 (research ticket vs. PM-owned investigation).

## How to resume
1. Read this file.
2. `bash .tigerteam/scripts/board-status.sh`
3. Process review/ first, then blocked/.
4. `git worktree list` — tigerteam/* entries are unmerged ticket branches.
5. `tigerteam up` (or `tigerteam worker run <worker> --once` to smoke first).
6. Continue from Next actions.
