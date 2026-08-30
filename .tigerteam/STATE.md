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

## Next actions
**Phase 2 complete for the providers we can reach.** ChatGPT quota is now
sourced honestly from rollouts (T-0004) and extra-usage credits are reported
correctly for both Claude and ChatGPT (T-0005). 60 tests, board drained.

Blocked on the human — see "Provider research" below for the detail:
1. **Grok** — open the billing/credits view in the Grok TUI once, then grep
   `~/.grok/logs/unified.jsonl` for the billing URL.
2. **Kimi** — run `kimi` once to refresh the expired token, then probe
   `/coding/v1/me` and capture a fixture.
3. **DeepInfra** — need to know where the tool should read the key from.

Then, per provider: capture a real response as a committed fixture (sanitised),
add a source + register it in `ProviderCatalog.all`, and ticket it C2.

Also still open (low priority):
4. Surface `severity` / `is_active` from Claude's `limits` array — the endpoint
   marks which window is binding and we ignore it.
5. Mac app/widget remain ON HOLD pending the human's UI work.

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

## CORRECTION (2026-08-29, later) — the ChatGPT verdict above was WRONG
The "ChatGPT / Codex — RESOLVED, no live endpoint is possible" section is
superseded. The human pushed back and was right. Two live routes exist:

1. **`codex app-server` (preferred, now T-0006).** The Codex CLI ships a local
   newline-delimited JSON-RPC server with a documented
   `account/rateLimits/read` method:
   `codex app-server --stdio`, send `initialize` (id 1), `initialized`, then
   `account/rateLimits/read` (id 2). Measured **0.24 s**. Returns
   `result.rateLimits` with `primary`/`secondary`
   (`usedPercent`, `windowDurationMins`, `resetsAt`), `credits`, `planType`,
   plus `rateLimitResetCredits`. **No bearer token is handled by us** and there
   is no bot-protected host in the path. Fixture committed as
   `Fixtures/codex-app-server-ratelimits.json`.
2. **`GET https://chatgpt.com/backend-api/wham/usage` → 200.** Works with
   `Authorization: Bearer` + `ChatGPT-Account-Id` from `~/.codex/auth.json`.
   Returns `rate_limit.{primary,secondary}_window`, `credits`, `plan_type`.

**Where my earlier analysis went wrong:** I concluded "Cloudflare blocks every
plain HTTP client". It does block `/backend-api/api/codex/usage` and
`/backend-api/codex/usage` (403 + HTML challenge), but `wham/usage` answers
200 — *the path was wrong, not the wall*. Note also that the 403 probes sent
`originator: codex_cli_rs` and a spoofed `User-Agent`, and the 200 probe sent
neither; the custom User-Agent is the likelier trigger. Lesson: do not
generalise a whole host as unreachable from two 403s on adjacent paths, and
strip unusual headers before concluding anything.

What stands from T-0004: the rollout reader is still the right *local*
fallback, and the honest "only after a Codex turn" status text is still
correct for that path. T-0006 adds the live source above it.

## Provider verdicts after the second research round
- **Grok — SOLVED, ticketed as T-0007.**
  `GET https://cli-chat-proxy.grok.com/v1/billing?format=credits` with
  `Authorization: Bearer <~/.grok/auth.json token>` and
  `x-grok-client-mode: grok-build` → 200. Gives `creditUsagePercent`, a weekly
  `currentPeriod`, `productUsage[]` breakdown, `onDemandCap/Used`,
  `prepaidBalance`. Fixture committed. The base host came from
  `~/.grok/logs/unified.jsonl` after the human opened the billing view — the
  `/v1` prefix was the missing piece; `/rest/*` and bare `/billing` are 404.
- **Kimi — NO usable endpoint found.** `api.kimi.com/coding/v1/me` returns 200
  but is **identity only** (user_id, nickname, phone number, user_level) — it
  carries PII and no quota, so we should not ingest it. Probed and 404: `/usage`,
  `/quota`, `/balance`, `/subscription`, `/plan`, `/limits`, `/me/quota`,
  `/oauth/usage`, `/plan/usage`. The `/usage`-shaped strings in the binary are
  bundler source paths, not routes. Kimi likely surfaces usage in-session only.
- **DeepInfra — NO usable endpoint found.** Key read from `<root>/.env`
  (`DEEPINFRA_KEY`). `api.deepinfra.com/v1/me` → 200 but identity/account flags
  only, no balance. Probed and 404: `/v1/billing`, `/dash/billing`,
  `/dash/balance`, `/dash/usage`, `/v1/credits`, `/v1/me/usage`, `/v1/account`,
  `deepinfra.com/api/billing`. `/v1/inference/usage` exists but rejects GET
  (405). **To unblock:** open the DeepInfra dashboard billing page with browser
  devtools and capture the XHR URL — the same trick that cracked Grok.

**Note on `<root>/.env`:** it holds `DEEPINFRA_KEY` and is deliberately masked
from workers by `worker.sb`. If a DeepInfra provider is ever built, it must
read the key from the **environment**, never by parsing `.env` — workers cannot
read that file and must not learn to.

## DeepInfra — RESOLVED (2026-08-29, third round)
Found via the vendor's own docs index (`https://docs.deepinfra.com/llms.txt`)
rather than by guessing paths — the paths are **not** under `/v1`:
- `GET https://api.deepinfra.com/payment/config` → `{"limit": -1.0}` (USD
  spending limit; negative = no limit). **200.**
- `GET https://api.deepinfra.com/payment/usage?from=current` → `months[]` with
  `total_cost` in **cents** and `interval` in **milliseconds**. **200.**
Key comes from the `DEEPINFRA_KEY` environment variable — never by parsing
`<root>/.env`, which is masked from workers by `worker.sb`.
DeepInfra is pay-as-you-go: no quota, no prepaid balance. Honest readouts are
month-to-date spend ($7.75 here) and usage against a limit when one is set.
Fixture: `Fixtures/deepinfra-usage.json` (trimmed).

## PAUSED 2026-08-29 — core language under review, board halted
The human asked whether the core should move from Swift to **Python** to make
it genuinely cross-platform (an Omarchy/Ubuntu UI, possibly a Windows widget),
then said to stop and summarise for a restart. **Nothing further was queued.**

State at the pause:
- Supervisor stopped (`tigerteam down`); `.tigerteam/STOP` present.
- T-0001..T-0005 accepted and merged on `master`. 60 tests green.
- **T-0006** (live ChatGPT via `codex app-server`) and **T-0007** (Grok
  provider) were written, then pulled back to `.tigerteam/board/drafts/`
  **unstarted** — do not re-queue them as-is if the core is rewritten. Their
  *content* (endpoints, mappings, gotchas) is still correct and worth mining.
- The killed T-0006 attempt produced no commits; its branch was deleted. No
  work lost.

**The durable asset is `PROVIDERS.md` at the repo root** — every endpoint,
credential path, response shape, dead end and gotcha, written to be
language-neutral. Plus four committed JSON fixtures that remain valid test data
in any language: `claude-usage-live.json`,
`codex-app-server-ratelimits.json`, `grok-billing-credits.json`,
`deepinfra-usage.json`.

Sizing for a port decision: `QuotaKit/Sources` is ~2,550 lines total, of which
only these are Apple-locked — `Support/Keychain.swift`,
`Support/SeverityColor.swift`, `Engine/QuotaEngine.swift` (Observation),
`Engine/WidgetRefresher.swift` (WidgetKit), `UI/Sparkline.swift`,
`UI/TufteTheme.swift`. The provider/model/format layer is plain Foundation.

Behaviours worth preserving in any rewrite (each cost a real bug to learn):
1. A reset window reports no reading, never `0%`.
2. Credentials addressed by explicit path, never a recursive key search.
3. Extra-usage credits honour an `enabled`/spendable flag before claiming a
   balance is available.
4. Live wins when it succeeds; otherwise show the cached reading *and* say why
   it is not live.
5. The most *actionable* failure is the one reported, not the last one.
6. Prefer a vendor's local CLI (`security`, `codex app-server`) over its
   private HTTP API.

## RESUMED 2026-08-29 20:12 — Go core port, phase 1 (GO-PORT.md)

**Decision (user, GO-PORT.md):** core moves to Go; Swift fetchers frozen as
reference semantics; `quotactl --json` is the parity oracle.

**Environment changes (PM, commit 795b44d):**
- Installed Go 1.27 via Homebrew (was absent — the one gap in GO-PORT.md).
- `worker.sb`: read+write carve-outs for `~/Library/Caches/go-build`, `~/go`.
- `test_cmd = "bash scripts/test-all.sh"` (Swift suite, then `go vet`+`go test`
  in `core/` once `core/go.mod` exists). Supervisor restarted after the change.
- Sandbox smoke (runner-exact params, real worktree path): vet/test/build/run
  and linux/amd64 cross-compile all OK. Go's temp dir is the darwin user TMP,
  which the runner already passes.
- `.gitignore`: `core/bin/`.

**Design decisions locked in tickets:**
- Status JSON v2 `{"state":…,"message"?}`; Swift decodes legacy + v2, encodes v2.
- Timestamps RFC 3339 UTC, no fractional seconds (Swift decoder rejects them).
- `source.ErrorKind` numeric value == reporting priority (0..4).
- `jsonx` is explicit-path only; no recursive search in the Go core, ever.
- Provider READMEs live in-package; T-0011 folds them into `core/README.md`
  (avoids two parallel tickets editing one file).

**Board:** T-0008 (scaffold, P0) → T-0009 Claude ∥ T-0010 Codex → T-0011 CLI.
Phase 2 (Grok, DeepInfra) and mac-app integration not yet ticketed.

**PM acceptance step after T-0011:** run `quotamon snapshot` vs
`swift run quotactl --json` on this Mac; every percentage must agree.

### Phase 1 landed — 2026-08-29 ~20:55
- T-0008 scaffold, T-0009 Claude, T-0010 Codex, T-0011 CLI/hybrid/waybar all
  accepted on master. Go suite: 9 packages green; Swift 62 tests green.
- **Parity verified by PM**: `quotamon snapshot` vs `quotactl --json` on this
  Mac — Claude 3/15/21, ChatGPT 70/27, identical. `waybar` and `check` work.
- Fleet cost so far negligible (see `tigerteam cost`).
- **Open bug T-0014 (P0/C1, in progress):** the default app-server runner
  closes stdin before the id:2 reply arrives; the server exits early. Rollout
  fallback covers it meanwhile, and the hybrid status says so honestly.
  Root cause was PROVIDERS.md (my text) saying "close stdin after writing";
  corrected in commit "PROVIDERS.md: app-server needs stdin held open".
- Two PM slips this session, both caught by workers/doc: a wrong wall-clock
  time in a T-0010 criterion (1788038896 s = 21:28:16Z) and the stdin rule.
- `ds` lane (pi/DeepSeek, user-added) fast-fails: model not found →
  `STOP.ds` present. User to fix or remove the profile.
- Phase 2 queued next: T-0012 Grok → T-0013 DeepInfra (serialised: both
  touch registry.go and waybar.go).
- Not yet ticketed (GO-PORT cutover steps 2–3): freeze notice in AGENTS.md
  is done; mac-app bundling of `quotamon` and deleting Swift fetchers await
  the user's go-ahead since App/ is on hold.

### Phase 2 + CLI landed — 2026-08-29 ~21:35
- Accepted: T-0012 Grok (ds lane's first landing), T-0013 DeepInfra, T-0014
  app-server stdin fix, T-0015 bare `quotamon` table + `--json`.
- Live on master: Claude, ChatGPT (app-server), Grok; DeepInfra live works
  (~4.7 s, `/payment/usage` is slow) but is dropped by the hybrid merge
  because it has zero windows by design → **T-0017 (P0/C1, in progress)**.
- User decisions on config (2026-08-29): JSON; API keys allowed in the file
  with 0600 enforced; config mandatory; `setup` auto-discovers known
  providers, shows findings, asks per provider, offers manual add. Tickets
  T-0018 (config + discover + registry gate) → T-0019 (setup/providers) →
  T-0016 (user docs, last so it documents the real first run).
- PM slips caught by workers this session: T-0010 wall-clock, T-0013
  timezone (I converted ms→Pacific), T-0012 `check --no-live` contradiction,
  T-0015 `depends_on` that was only a scope collision (user caught that one).
  Lesson: compute timestamps with `date -u -r`, never by hand; and for scope
  collisions prefer disjoint files over `depends_on`.
- `ds` lane fixed: pi needed a custom provider in `~/.pi/agent/models.json`
  (`deepinfra`, `apiKey: "$DEEPINFRA_KEY"`); model id is
  `deepinfra/deepseek-ai/DeepSeek-V4-Flash-0731`.

### Config + setup landed — 2026-08-29 ~22:05
- Accepted T-0017 (hybrid keeps credits-only providers), T-0020 (zero-balance
  line), T-0018 (config + discover + registry gate), T-0019 (setup/providers).
- Verified by hand: no config → exit 3 + hint; waybar "run setup" payload;
  0644 config with a key refused with the exact chmod command; interactive
  setup transcript; `providers` table. 17 tickets done.
- Remaining: T-0016 docs (in flight). After it: the board is drained.
- Not ticketed / needs the user: Mac app cutover (App/ on hold); Windows;
  history/pace in Go; AUR packaging; Kimi (no endpoint).

### Board drained — 2026-08-29 ~22:20
- T-0016 docs accepted (+ PM commit fixing title, a leaked ticket phrase,
  and the sample table). 18 tickets done; full suite green on master
  (62 Swift tests, 13 Go packages). Fleet cost: 36 attempts, 2h34m wall,
  22.6M tokens in (92% cached), $0.12 reported (most engines report no cost).
- Supervisor left running, idle. `tigerteam down` when not needed.
- Next candidates (need user decision): Mac app cutover (GO-PORT step 3,
  `App/` on hold); AUR/curl install; history/pace in Go; Windows tray;
  Kimi endpoint capture.

### Wave 4 — 2026-08-30 ~00:05
- Accepted since last note: T-0021 table (bars, colour), T-0022 Kimi provider
  (endpoint found via NODE_OPTIONS hook on the TUI: `coding/v1/usages`),
  T-0023 DeepInfra parallel calls, T-0024 setup --yes adoption, T-0027
  DeepInfra balance (remaining = −stripe_balance − recent, user-verified),
  T-0025 last-reading cache + Kimi refresh by launching the CLI (home dir,
  own 20 s clock, 25 s pre-fetch stage). 24 done.
- User decisions: no OAuth token rotation by quotamon; Kimi refresh = launch
  the CLI briefly via script(1) pty; refresh rarely (cache while token stale).
- In flight/queued: T-0026 refresher hygiene (pgid kill, flock, orphan sweep,
  rate cap) — P0; T-0028 fetch budget 15 s + --timeout + cents rounding.
- Findings worth keeping: Kimi TUI blocks on "Trust this folder?" outside
  trusted cwd; `kimi -p "/usage"` spends tokens; DeepInfra latency 2–7 s.

### Board drained — 2026-08-30
- 26 tickets accepted. Final wave: T-0026 (Kimi refresher process hygiene:
  pgid group-kill, flock, ps-based orphan sweep, 10-min cap, Windows fallback),
  T-0028 (fetch budget: --timeout / QUOTA_MONITOR_TIMEOUT, >0 rule, DeepInfra
  per-call 12s, cents-rounded balance).
- Replicate: NOT added — API token is identity-only, credit is cookie-gated
  (dashboard). Excluded by the no-cookie rule; documented in PROVIDERS.md.
- Full suite green (62 Swift + all Go pkgs); make matrix builds darwin/arm64,
  linux/amd64, linux/arm64. Supervisor down. Board idle.
- Open (needs user): Mac app cutover (App/ on hold), history/pace in Go,
  Windows tray, AUR/install packaging.
