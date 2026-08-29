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
- 2026-08-29 — **T-0001, T-0002, T-0003 all accepted and merged to master.**
  Board drained, no worktrees, `tigerteam check` 15 ok / 0 warn / 0 fail.
  Tests 37 -> 50, all passing. Total fleet spend: 8 attempts, ~29m, $0.12.
  Verified by hand on master: `quotactl` prints both providers; Claude reads
  `live` (no Keychain prompt) and the previously-missed `Fable wk 20.0%`
  scoped window now appears as the most constrained reading.

## Next actions (phase 1 complete — these await the human's call)
1. **Codex/ChatGPT live source returns HTTP 404.** Local rollout still works.
   Needs endpoint research; deliberately not a C2 ticket.
2. **Credits display overstates headroom.** `spend` maps to
   `credits.balance` = limit - used, so a `max` account renders
   `credits 20.00 remaining` even though the same payload says
   `spend.enabled: false` / `disabled_reason: "out_of_credits"` (credits are
   not actually usable). My spec for T-0003 asked for exactly this, so the
   worker was correct; the *spec* was wrong. Fix: honour `enabled` before
   showing a balance.
3. Optional: surface `severity` / `is_active` from the `limits` array — the
   endpoint marks which window is the binding one, and we currently ignore it.
4. Mac app/widget remain ON HOLD pending the human's UI work.

## How to resume
1. Read this file.
2. `bash .tigerteam/scripts/board-status.sh`
3. Process review/ first, then blocked/.
4. `git worktree list` — tigerteam/* entries are unmerged ticket branches.
5. `tigerteam up` (or `tigerteam worker run <worker> --once` to smoke first).
6. Continue from Next actions.

## Environment incidents (2026-08-29) — all found by workers blocking, not guessing
Worker `codex` blocked T-0001 with three infrastructure failures. All were real
and all were mine; its code was fine (42 tests green once the sandbox worked).

1. **`sandbox_apply: Operation not permitted`** — SwiftPM nests its own
   `sandbox-exec`; macOS refuses nested Seatbelt. Handled by `--disable-sandbox`
   in `test_cmd`.
2. **`codesign` EPERM / `git fatal: Invalid path '/Users/d/b'`** — one root
   cause. `worker.sb` denied all of `$HOME` and re-allowed only ROOT, never the
   ancestors between them; path resolution stats every component. Any repo
   nested below `$HOME` broke git, and broke `codesign`, which fails every
   `swift build` producing an executable. Fixed in `f7d5b10` by allowing
   path-component *metadata* everywhere (contents still denied; smoke still
   passes every boundary check). **My first smoke test missed this because it
   built in `/tmp`, which has no denied ancestors — test in a real worktree.**
3. **`TIGERTEAM_TEST_CMD not set`** — the supervisor was started before
   `test_cmd` existed in `tigerteam.toml` and injects env at launch. Restarted
   (pid 5605). *Any config change needs a supervisor restart to reach workers.*

**Known machine quirk:** `run-tests.sh`'s fallback (`tigerteam config get
test_cmd`) cannot work inside the sandbox on this Mac — `tigerteam` is a uv
*editable* install resolving to `/Users/d/w/TT/tigerteam/src`, outside the
profile's allowed tree, so it dies with `ModuleNotFoundError`. Workers depend
entirely on the supervisor's injected `TIGERTEAM_TEST_CMD`. If a worker ever
reports the var missing again, restart the supervisor first — do not widen the
sandbox to another repo.

## Provider research (PM, 2026-08-29) — do not redo this by hand
Established by direct probing with the user's real credentials. Endpoints and
verdicts, so no future PM or worker repeats the work.

### ChatGPT / Codex — RESOLVED, no live endpoint is possible
- `https://chatgpt.com/backend-api/api/codex/usage` is the **correct** path (it
  appears verbatim in the Codex binary), but the host is behind **Cloudflare bot
  protection**: any plain HTTP client gets **403 + an HTML challenge**. The
  "404" we used to display was that wall.
- Codex never calls a usage endpoint. It reads limits from **response headers**
  (`x-codex-active-limit`) during real API turns and writes the resulting
  `rate_limits` into session rollouts.
- Therefore `CodexLocalSource` is the *only* source of ChatGPT quota, accurate
  as of the last Codex turn. T-0004 made the live source opt-in behind
  `QUOTA_MONITOR_CODEX_USAGE_URL` and fixed the misleading status text.
- **Policy: do not attempt to bypass the bot protection** (no browser UA, no
  cookies). Matches the standing comment in `ClaudeSources.swift`.
- Token validity is not the issue: `~/.codex/auth.json` held a token valid to
  2026-09-02 throughout.

### Grok / xAI — BLOCKED on one unknown base URL
- Auth: OIDC token at `~/.grok/auth.json`, under a
  `https://auth.x.ai::<client-id>` key, field `key` (a JWT, ~6h lifetime,
  carries a `tier` claim). Refresh token alongside it.
- `GET https://api.x.ai/v1/me` → 200 but **identity only** (user_id, team_id,
  zdr_status). No quota. `/v1/usage`, `/v1/credits`, `/v1/rate-limits` → 404.
- `GET https://grok.com/rest/subscriptions` → 200, but it is **Stripe
  subscription history** (tier, status, invoice/price ids). No usage numbers,
  and it carries billing identifiers we should not ingest.
- The binary contains the real target: **`/billing?format=credits`**, sent with
  `Bearer` + header `x-grok-client-mode`, returning
  `creditUsagePercent`, `monthlyLimit`, `onDemandCap`, `onDemandUsed`,
  `prepaidBalance`, `includedUsed`, `totalUsed`, `subscription_tier`,
  `billingPeriodStart`. Error strings say it "requires auth with grok.com".
- **The base URL is assembled at runtime and could not be determined.** Ruled
  out: `grok.com/rest/billing`, `/rest/billing/credits`,
  `/rest/app-chat/billing`, `/rest/user/billing`, `/rest/payments/billing`,
  `grok.com/api/billing`, `code.grok.com/billing`, `api.x.ai/billing`.
- **To unblock:** have the human open the billing/credits view in the Grok TUI
  once; the request URL then lands in `~/.grok/logs/unified.jsonl`, which is
  greppable for `billing`.

### Kimi — BLOCKED on an expired token
- Base `https://api.kimi.com/coding/v1` (from `~/.kimi-code/config.toml`).
- Credentials at `~/.kimi-code/credentials/kimi-code.json`
  (`access_token`, `refresh_token`, `expires_at`, 900s lifetime).
- `GET /coding/v1/me` → **401** (route exists); `/usage`, `/quota`,
  `/users/me/balance` → 404. The stored token was ~30h expired at probe time.
- **To unblock:** run `kimi` once to refresh, then re-probe `/coding/v1/me` and
  capture the response shape as a fixture.

### DeepInfra — BLOCKED, no credential
- `DEEPINFRA_KEY` is not present in the PM environment and no key file was
  found. Nothing can be verified. **Ask the human where the tool should read it
  from** before writing a ticket.

### Open design question for all three
Grok and Kimi may, like ChatGPT, expose no usage *number* at all — their CLIs
may learn limits only from response headers on real turns. If so, the honest
deliverable is a balance / tier line, not a percentage. Decide only after
seeing a real response; do not ship a provider that silently reports nothing.
