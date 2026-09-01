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

### macOS app rebuilt — 2026-08-30
- 30 tickets done. T-0029 (core setup surface), T-0030 (QuotamonRunner +
  engine ingestion), T-0031 (macOS app + widget + bundling, opus lane),
  T-0032 (quotamon --demo). Replicate excluded (cookie-gated).
- App: MenuBarExtra console-style table, severity bars, Refresh button,
  first-run SetupView (drives `discover --json` + `config set`), widget reads
  the App Group snapshot. Bundles a universal `quotamon` in Contents/Resources
  via a project.yml preBuildScript. Builds green (xcodebuild), renders both
  panel PNGs headlessly.
- README now leads with docs/console.png + docs/app-light.png (sample data);
  scripts/screenshots.sh regenerates all three.
- **opus-lane gotcha:** sandbox_profile="" turns Seatbelt off, but the Claude
  Code permission allowlist (rtk hook, ~/.claude/settings.json) still denies
  xcodegen/xcodebuild/make/swift-build to a headless worker. So the opus lane
  can WRITE app code but cannot BUILD it; the PM verifies the build outside the
  worker. To let the lane self-verify, the user must allowlist those commands.
- Supervisor down, board idle. Open: iOS/CloudKit, history/pace, Windows,
  AUR/install packaging, App Group signing for the widget (needs a team).

## 2026-08-30 — feature/console-look (app panel = the console, literally)
- Product owner: the terminal screenshot (`docs/console.png`) looks better than
  the Tufte panel; the app must adopt the exact console look — same face (SF
  Mono 13pt), same `█░` bars/columns/colouring, same spacing — with a light
  counterpart following the system appearance. Header/footer/setup stay,
  restyled.
- **Decisions:** (a) the Go `table.go` is the single reference; `ConsoleReport`
  (old quotactl layout) is NOT it. (b) Port it to Swift as `ConsoleTable`
  in QuotaKit with a golden test pinned to the Go demo output, so the app
  re-renders as `now` ticks (running `quotamon` for text would freeze
  countdowns). (c) Swift `Credits` gains `spend` — it was silently dropping
  the core's field, so DeepInfra's balance/spend rows could never render.
  (d) App gets its own `ConsoleTheme`; `Tufte` stays for the widget. Panel
  shows providers in snapshot order like the console (no re-ranking).
  (e) Everything lands on **`feature/console-look`** (user wants an easy
  undo); nothing touches master until the user approves the look.
- Tickets: **T-0033** (C2, sandboxed, QuotaKit) → **T-0034** (C2, opus lane,
  App/, depends on T-0033). Expect opus to hand T-0034 off unbuilt (allowlist
  blocks xcodebuild); PM builds + renders + regenerates screenshots at review.
- Finish: `tigerteam feature merge console-look` only on the user's say-so;
  `tigerteam feature rm console-look` to discard.
- 2026-08-30 15:50 — **T-0033 and T-0034 accepted on `feature/console-look`**
  (2 tickets, 3 attempts, ~13 min worker time, $3.56 on opus + one uncosted
  codex run). PM built/rendered T-0034 by hand both times (opus lane still
  can't run xcodebuild): first render had every countdown one unit low
  (sample offsets exact, render a few ms later → truncation); reworked with
  30 s slack; second render matches `docs/console.png` line for line.
  `docs/app-{light,dark}.png` + `menubar.png` regenerated on the feature
  branch. **Waiting on the user's verdict** — merge with
  `tigerteam feature merge console-look`, discard with `feature rm`.
  Master untouched. Note for later: the T-0034 grep criterion
  `rankedProviders` was over-broad (MenuBarIcon legitimately ranks) — waived.
- 2026-08-30 16:00 — **console-look MERGED to master** (`2517bce`, user: "bloody
  perfect"). `tigerteam feature merge` refused because it assumes trunk =
  `main` and this repo uses `master`; merged by hand with `git merge --no-ff`
  on the clean root checkout, then removed the feature branch/worktree.
  App rebuilt + relaunched from master via `scripts/build.sh`. README
  screenshots already regenerated on the branch. Mac app is no longer "on
  hold": its panel is the console table (`ConsoleTable` in QuotaKit, SF Mono
  `ConsoleTheme` in App/). Widget still uses `Tufte`.
- 2026-08-30 16:20 — **T-0035 accepted on `feature/console-widget`** (opus,
  1 attempt). Root cause of the "app font looks heavier" report: SFNSMono.ttf
  is a variable font whose default instance is Light; the console screenshot
  is drawn at that default, the app asked for Regular. Now `.light` (ink 816
  vs console 573 vs old 961 — remainder is CoreText smoothing, not weight).
  `ConsoleTheme` + new `ConsoleWidgetView` live in QuotaKit/UI; Tufte and
  Sparkline deleted; widget = console rows (small 17 cols / medium 37 /
  large = full table), unit-tested; DesignSnapshot renders six widget PNGs.
  PM built app+widget, rendered, regenerated docs (+ `docs/widget.png`,
  README line). **Awaiting the user's merge verdict**; merge by hand
  (`git merge --no-ff`, see memory: feature merge assumes `main`).
- 2026-08-30 16:30 — **console-widget MERGED to master** (`05793f9`, user:
  "merge it"); feature branch removed; app rebuilt + relaunched. Console look
  is complete: panel + widget in SF Mono Light. Widget needs the user's team
  id in `Config/Signing.xcconfig` (+ `-AppGroup` variant) to be installable.
  Board drained (33 done).
- 2026-08-30 18:20 — **T-0036 accepted on master**: menu bar icon bars now in
  snapshot order (= panel order); previously `rankedProviders`, which the user
  read as misleading. PM built, re-rendered `docs/menubar.png`, relaunched.
  Board drained (34 done). User asked about shipping DMGs from GitHub —
  answered (tag-triggered Actions on macos runner; Developer ID +
  notarytool needed for Gatekeeper); not ticketed unless asked.
- 2026-08-30 18:40 — **T-0037/T-0038/T-0039 accepted on master.** Claude
  credits: `spend.limit` is the monthly extra-usage cap, `spend.balance` the
  prepaid balance — both parsers (Go + frozen Swift reference) had rendered
  `limit − used` as a balance ("20.00 (not enabled)" on an account with $0).
  Now `spend  $0.00 of $20.00 this month`; verified live on the owner's
  account. Release pipeline: `.github/workflows/release.yml` (tag `v*` →
  macos-15, universal Release build, `scripts/package.sh` DMG + quotamon
  binaries + SHA256SUMS, gh-release; optional Developer ID sign/notarize via
  secrets), `make matrix` now builds darwin/amd64. T-0039 needed one rework
  (`format()`-quoted flags, `v` prefix, raw notary key, invented repo URL).
  **Untested in CI until the owner pushes a tag** — pushing is theirs.
  App rebuilt + relaunched. Board drained (37 done).
- 2026-08-31 — **PR #1 (Omarchy bar plugin) reviewed, fixed, merged.** /code-review
  found 15 defects (bash -lc injection/parse breakage, NaN interval → fetch
  storm, silent staleness, icon re-ranking, a third untested derived layer
  diverging from the console). T-0040 (codex, feature/omarchy-pr1 cut from the
  PR head) fixed all: argv exec, exit-code error handling + staleness banner,
  Go-transcribed sortedWindows/tightest tie-break/creditLines/countdown/age,
  snapshot-order icon, plus `omarchy/model_test.mjs` wired into test-all.sh
  behind a node guard (JS derived layer now pinned like Swift's). One blocked
  question (demo fixture predates branch — approved inline data). Merged to
  master and pushed (user authorised push: "streamline"); PR closes as merged.
- 2026-08-31 — **Stress-review fix wave COMPLETE: T-0045..T-0056 all accepted
  on master** (12/12; one rework: none — clean wave; ds carried 0048/0056,
  codex/luna/opus the rest). Landed: usable-readings exit code; hybrid cache
  as live-failure fallback + 15-min early-serve cap; api_key redacted from
  config JSON + --api-key-stdin (app uses it); setup refuses broken configs;
  codex resets_at + resilient rollout scan + deflaked app-server test;
  DeepInfra count/unknown-money honesty; grok label-from-period; discover
  hardening; waybar parity + clamp; Swift history nil-skip + full short-name
  catalog; runner 45s + posix_spawn process-group kill; widget credits rows +
  staleness marker; SetupView merges saved config; SHA256SUMS + build.sh
  fixes; omarchy watchdog. PM built+rendered T-0054/55 by hand. App rebuilt
  from master. NOT pushed — awaiting user.
- 2026-08-31 — **Omarchy zero-friction install shipped (T-0060)**: plugin
  published to github.com/ubyjvovk/quotamon-omarchy via
  `scripts/publish-omarchy-plugin.sh` (subtree split of omarchy/, force-push;
  source of truth stays here). Install = `omarchy plugin add <url>` → click
  the bar icon → "Install quotamon" (bundled checksum-verified
  fetch-quotamon.sh; tamper case verified to abort preserving the old
  binary). Retro updated (session-2). Supervisor wedged + restarted once
  (unclaimed-ticket symptom; manual `worker run --once` as fallback worked).
- 2026-08-31 — **RunInfra provider landed (T-0062, ds + opus rework)**: sixth
  provider, live-only credits/spend from api.runinfra.ai/v1/credits (Bearer
  RUNINFRA_TOKEN; cents ints; available_cents = balance; hard cap → "Cap"
  window, soft/absent cap → none). Rework was a PM scope miss: the
  known-provider map in config.Default() wasn't in scope, so `config set
  runinfra` failed. Live-verified on the real account; enabled in the real
  config via --api-key-stdin; app rebuilt. PROVIDERS.md documents the
  endpoint. NOT pushed yet.
- 2026-08-31 — **OpenRouter + DeepSeek providers landed (T-0063, codex, one
  shot)**: eight providers total. OpenRouter live-verified with the owner's
  key (credits shape confirmed against the real endpoint pre-landing; spend
  is LIFETIME, labelled "all time"; /api/v1/key carries usage_monthly if a
  monthly row is ever wanted). DeepSeek fixture-tested, live-verify pending a
  DEEPSEEK_KEY. Z.ai: NO documented quota API (docs index confirms) — needs a
  Kimi-style dashboard/XHR capture; ZAI_API_KEY is in the shell env, owner to
  choose the capture route. AGENTS.md now carries the six-place provider
  checklist. App rebuilt with OpenRouter enabled in the real config. NOT
  pushed.
- 2026-09-01 — **Packaging wave planned: T-0064..T-0068 (CalVer + version
  reporting + Omarchy manifest).** Owner brief: every component (console, Mac
  app, Omarchy plugin) on CalVer `2026.9.1`; the app and the plugin must report
  their own version *and* the underlying quotamon version in "about"; the
  plugin manifest needs a correct version, dependencies, and better description
  text. Four owner decisions taken up front (asked, not guessed):
  (a) CalVer is **`YYYY.M.MICRO`** — micro = the Nth release *within that
  month*, not the day (2026.9.1 → 2026.9.2 → 2026.10.1);
  (b) the plugin id is renamed `quotamon` → **`ubyjvovk.quotamon`** to match the
  Omarchy spec's namespacing rule — a deliberate break of the install path
  `~/.config/omarchy/plugins/<id>/`;
  (c) Omarchy's manifest schema has **no documented `dependencies` field**
  (verified against the develop/publish guides and manual/32-shell-plugins.md),
  so ours is an informational block **plus a runtime check in the panel** —
  the declaration is enforced by us, not by Omarchy;
  (d) MIT `LICENSE` added at the repo root and copied into `omarchy/` (the
  plugin is subtree-published, so a root-only licence never reaches it);
  `"license": "MIT"` goes in the manifest, which the develop guide lists as
  required.
- **PM design decisions (C3 → C2) for that wave:**
  - `VERSION` at the repo root is the single source of truth. The Go binary is
    stamped at link time (`-X quotamon/internal/version.Value`, default `dev`);
    `project.yml` and `omarchy/manifest.json` must hold the literal (the
    manifest is subtree-published, so it cannot be generated), which is why
    `scripts/set-version.sh` is the only writer and `scripts/check-versions.sh`
    makes drift a **test-suite failure**.
  - Two different mechanisms for "underlying quotamon version", each chosen for
    its consumer: the **Mac app runs `quotamon --version`** through the existing
    `QuotamonRunner` (honest about the binary actually inside the bundle, and
    correct before the first snapshot); the **Omarchy panel reads the new
    top-level `version` key in the snapshot JSON** it already parses (no second
    subprocess in QML). Both surfaces are therefore independent of each other.
  - All plugin logic lands in `Model.js` behind node tests — nobody on this
    board can run Hyprland, and T-0040's lesson was that an untested derived
    layer in QML diverges. `Panel.qml` renders only.
  - A `quotamon` older than 2026.9.1 emits no `version` key at all, so its
    absence *is* the "too old" signal the panel warns on.
  - `README.md` is deliberately owned by **one** ticket (T-0068, a C1 doc-sync
    gated on the other four) so the parallel tickets never collide on it.
  - Chain: T-0064 (core) → T-0065 (packaging scripts + project.yml + manifest
    version) → {T-0066 (Mac About, `assignee: opus`, `capability: [macbuild]`),
    T-0067 (plugin manifest + footer)} → T-0068 (README).
  - Expect T-0066 to hand off unbuilt (the opus lane still cannot run
    xcodebuild); PM builds, renders and verifies the About panel at review.
  - Known gap noted while surveying: `README.md` links to `core/README.md`,
    which does not exist — folded into T-0068 as a link fix, not a new file.
- 2026-09-01 — **Packaging wave COMPLETE: T-0064..T-0069 all accepted on master
  (6/6, zero reworks).** Every component now reports CalVer `2026.9.1` from one
  `VERSION` file. Landed:
  - **Core**: `core/internal/version.Value` stamped at link time by
    `core/Makefile` from `../VERSION` (defaults to `dev`, so a bare
    `go build ./...` is honestly labelled); `quotamon --version` works in any
    argv position; snapshot JSON carries a top-level `version` key through the
    custom `MarshalJSON`.
  - **Packaging**: `scripts/set-version.sh` (sole writer, CalVer-validated,
    idempotent) + `scripts/check-versions.sh` (first step of `test-all.sh`, so
    drift is a suite failure); `build.sh`/`release.sh` read `VERSION`;
    `release.sh --next` suggests the month's next micro; the release workflow
    checks out first, uses `VERSION` for dispatch builds, and refuses a tag
    that disagrees with the file.
  - **Mac app**: `QuotamonRunner.coreVersion()` runs `quotamon --version`;
    About shows `quotamon core 2026.9.1` above the description, cached per
    launch (the *rendered line* is cached, so a failing bundle never respawns
    the binary on each open).
  - **Omarchy plugin**: id `quotamon` → **`ubyjvovk.quotamon`** (install path
    break, owner-approved), `license: MIT`, informational `dependencies`
    block, rewritten top-level and barWidget descriptions; panel footer
    `Quota Monitor 2026.9.1 · quotamon 2026.9.1` plus a too-old warning. All
    logic in `Model.js` behind node tests; `Panel.qml` reads the manifest once
    via XMLHttpRequest (status 0 or 200 for `file://`) and renders bindings.
  - **MIT `LICENSE`** at the root and a byte-identical `omarchy/LICENSE` (the
    subtree split would otherwise publish the plugin unlicensed).
  - **README** doc-synced; the dead `core/README.md` link fixed (not created).
- **PM verification notes for that wave** (what was checked by hand, not
  trusted from reports): built stamped + unstamped Go binaries and confirmed
  `quotamon 2026.9.1` / `quotamon dev`; re-ran the check-versions negative test;
  exercised every `Model.js` branch directly — `compareVersions("2026.10.1",
  "2026.9.9") === 1` (numeric, not lexical: a string compare would call October
  older than September) and a `dev` core produces **no** false "too old"
  warning; confirmed `cache.Store` holds per-provider readings, not whole
  snapshots, so a snapshot's `version` always reflects the running binary.
- **Two gaps found and handled, not papered over:**
  (a) T-0065 regressed `release.sh` to `cat VERSION`, which only works from the
  repo root — my spec never asked for cwd-independence, so rather than reject I
  filed **T-0069** (C1, one line, `ROOT` resolution copied from build.sh) and
  accepted it the same cycle;
  (b) **the macOS About panel was never visually confirmed.** It is now ordered
  front from inside a `Task` (it must await the subprocess), one run-loop turn
  after `NSApp.activate`. `osascript` lacks assistive access on this machine, so
  a menu-bar extra cannot be driven programmatically — **the owner still owes a
  5-second eyeball**: click the icon → About, confirm the panel comes forward
  and reads `quotamon core 2026.9.1`. If it opens behind other windows the fix
  is small, but nobody has proven it does not.
- **Not verified anywhere (structural, unchanged):** the Omarchy QML cannot run
  on this host. `Model.js` is node-tested; the footer's actual appearance in
  Hyprland is unproven.
- **NOT pushed and NOT tagged.** `scripts/release.sh` was only ever read and
  `--dry-run`/`--next` exercised; cutting `v2026.9.1` is the owner's call.
- 2026-09-01 — **T-0070 accepted (`bcdfe8c`): the omarchy publish path now cuts
  releases.** Found while the owner asked why the plugin repo showed no release
  and a version-1 manifest. Confirmed against the GitHub API:
  `ubyjvovk/quotamon-omarchy` had **0 tags, 0 releases**, `master` at `1c076f6`
  (the T-0061 subtree split, 2026-08-31T19:42Z), publishing
  `"id": "quotamon"` / `"version": "1.0.0"` — i.e. every omarchy change from
  T-0062..T-0069 was unpublished, and the published README's own
  `omarchy plugin enable ubyjvovk.quotamon` line did not match the published
  manifest id. The main repo was fine (`v2026.9.1` released by Actions).
  Immediate cause: `publish-omarchy-plugin.sh` was never re-run after the wave.
  Structural cause: it was a bare split + force-push — no tag, no release, no
  version gate, and nothing in the documented release path mentioned it.
  Landed: the script resolves `ROOT`, runs `check-versions.sh` as a drift gate,
  probes the remote for `v<VERSION>` (a failed probe degrades to `unknown` and
  never fails the run, so sandboxed lanes can still `--dry-run`), reports the
  tag state, and pushes the tag **only when absent** — a published tag is never
  moved, so it keeps meaning the bytes it meant. New
  `omarchy/.github/workflows/release.yml` rides the subtree split into the
  plugin repo root, where a `v*` tag verifies itself against `manifest.json`,
  zips the plugin and cuts a release; it is inert here because GitHub only reads
  root workflows. `README.md` now documents one four-step sequence:
  `set-version.sh` → commit → `release.sh` → `publish-omarchy-plugin.sh`.
- **PM verification for T-0070** (beyond the worker's report, which ran under a
  sandbox with no network and so could only ever see `unknown`): confirmed the
  `#contributing--for-agents` anchor resolves to a real heading; proved by
  experiment that `zip -r … -x '.git/*' '.github/*'` emits **no** `.git/` or
  `.github/` directory entries, so an unzipped asset is not a broken git repo;
  re-ran the workflow's `sed` against the real manifest (`2026.9.1`); and ran
  the accepted script on master with real credentials —
  `Tag v2026.9.1: will be created`, exit 0, split `88136ca` (byte-identical to
  the worker's split sha). The remote probe therefore works in production, not
  just in the degraded path.
- 2026-09-01 — **PUBLISHED, owner-approved in session.** Ran
  `scripts/publish-omarchy-plugin.sh` for real. `quotamon-omarchy` master moved
  `1c076f6..88136ca` (a fast-forward — the force flag was not needed) and the
  new tag `v2026.9.1` was created. The plugin repo's own workflow fired on that
  tag and succeeded on its first ever run (actions/runs/33512358812), cutting
  **release `v2026.9.1`**, the repository's first, with asset
  `quotamon-omarchy-2026.9.1.zip` (15,635 bytes).
  Verified after the fact: the published `manifest.json` now reads
  `"id": "ubyjvovk.quotamon"` / `"version": "2026.9.1"` / `"license": "MIT"`, so
  the published README's `omarchy plugin enable ubyjvovk.quotamon` line finally
  matches the manifest it ships with; and the downloaded asset contains exactly
  the nine plugin files with no `.git/` or `.github/` entries, so an unzip is
  directly installable. The packaging effort is closed — every surface
  (core, CLI, Mac app, plugin manifest, plugin panel footer, both repos'
  releases) reports `2026.9.1` from one `VERSION` file.
- 2026-09-01 — **`omarchy/preview.png` added and published** (`87540f4`;
  plugin repo `88136ca..c4196b1`). Omarchy reported the plugin had no preview
  image. The convention, confirmed against the Omarchy docs: a file literally
  named `preview.png` beside the QML, **declared by no manifest key** — the
  filename is the whole contract, which is why nothing in `manifest.json` could
  have flagged it missing. That contract is now recorded in `omarchy/README.md`
  so a later rename does not silently blank the marketplace listing. The image
  is the owner's own capture of the panel, 630x584, 57.6 kB, committed verbatim.
  Verified live: `raw.githubusercontent.com/.../master/preview.png` serves
  HTTP 200 `image/png`.
  The publish took the `present` branch exactly as designed — **master only, the
  `v2026.9.1` tag untouched** — so that release's zip keeps the bytes it was cut
  from and does not carry the preview. Deliberate: both the marketplace and
  `omarchy plugin add` read a clone of master, and bumping the project's CalVer
  (a new DMG, a new main release) for a screenshot would be disproportionate.
  Also confirmed by this push that the plugin workflow's `on: push: tags`
  trigger does **not** fire on a branch push — the run count stayed at 1.
- **Open, owner-side:** the preview shows Grok in its red stale state ("its
  window has since reset"). Honest, but an odd shop window; a capture with both
  providers live would sell it better. Cannot be taken here — Omarchy is not on
  this machine. Also still open: the 5-second eyeball on the macOS About panel.
- 2026-09-01 — **Grok was frozen on stale cache on every machine; root cause
  found, fixed and verified live (T-0071, T-0072 accepted).** The owner reported
  it stuck in the Omarchy panel and then locally "even tho i just spent some
  tokens". It was never a token, network or refresh fault:
  - `~/.grok/auth.json` was valid for another six hours, and a direct call to
    `cli-chat-proxy.grok.com/v1/billing?format=credits` returned **200** — with
    **no `creditUsagePercent` and no `productUsage`**. The weekly period had
    reset that morning at 11:09:21Z and usage since rounded to zero; the API is
    protobuf-JSON, which omits default scalars and empty repeated fields. Zero
    usage is transmitted as silence.
  - `grok.Snapshot` treated that silence as corruption and returned
    `ok = false`, so every live fetch was judged malformed and hybrid fell back
    to a cache written **four minutes before the reset**, pinned at 100%.
    Verified: `quotamon --json --fresh` returned `origin: local`,
    `usedPercent: 100`, `resetsAt` already in the past.
  - **T-0071**: absence inside a recognised envelope now means zero, with a
    `productUsage` sum as a defensive fallback and the real captured response
    committed as `grok-billing-credits-zero.json`. Verified after the merge on
    master against the live endpoint: `origin: live`, `status: ok`,
    `usedPercent: 0`, `resetsAt: 2026-09-08T11:09:21Z`.
  - **T-0072**: `fallbackReading`'s window-rolled-over branch returned before it
    ever read `liveError`, so the one actionable sentence
    ("Unrecognised response from Grok billing endpoint") was computed and
    discarded — which is exactly why this looked like a refresh bug and cost an
    hour. The reason is now appended. Also replaced the hardcoded ``open `kimi` ``
    in `PreFetch` with a `RefreshHint` field; **the field is unwired** —
    `registry.go` still needs `RefreshHint: "kimi"` on the Kimi provider, a
    one-line PM follow-up.
- **Consequence not yet handled:** the fix reaches the owner's Linux box only
  through a release — `omarchy/fetch-quotamon.sh` pulls from
  `releases/latest/download`. Fixing Grok there means `set-version.sh 2026.9.2`
  → commit → `release.sh` → `publish-omarchy-plugin.sh`, which would also carry
  `preview.png` into a fresh plugin release. Owner's call.
- 2026-09-01 — **T-0073/T-0074 filed: drop the superseded Swift core.** Owner:
  "we probably should drop the deprecated core swift files since the core is all
  go now". PM survey first, so the deletion set is evidence rather than
  intuition: `QuotaEngine.refresh(fresh:)` returns early whenever `runner` is
  non-nil, and the only real engine is `QuotaEngine(runner: CoreBinary.runner)`
  — so the entire `Providers/` layer is already **unreachable at runtime**. A
  symbol-by-symbol scan over Sources/App/Widget/Tests shows `Providers/` is
  referenced only by the library, its own tests and `quotactl`; the `Claude` and
  `Keychain` hits in `App/` are prose in comments, not type references. Cascade:
  `ConsoleReport` (tests + quotactl only), `Keychain` (Providers + ConsoleReport
  + tests), `JSONValue` (Providers only). `PreviewData` needs `Claude.providerID`
  / `Codex.providerID` replaced by the literals the Go core emits.
  **Constraints written into the ticket:** the `Fixtures/` directory must not be
  touched — `core/internal/fixtures/fixtures.go` loads ten fixtures from it by
  name and `ConsoleTableTests` loads `quotamon-demo.json` through
  `Bundle.module`, so a tidy-up there breaks the **Go** suite. `GO-PORT.md` is a
  dated record and must not be edited to match the present. `AGENTS.md` is the
  PM's file and the PM updates it at accept. No worker on this board can build
  the Xcode targets, so the ticket stops at `swift build` and the PM verifies
  the app and widget by hand.
- 2026-09-01 — **Swift provider layer and `quotactl` deleted; `GO-PORT.md`
  retired (T-0073, T-0074 accepted).** 2,212 lines gone across 15 files, plus
  the 230-line port document. Swift tests 95 → **51**: the 44 that went tested
  the deleted fetchers; the 27 covering the GUI path
  (`ConsoleTableTests`, `ConsoleWidgetTests`, `QuotamonRunnerTests`) are
  untouched and green, as is `Fixtures/`.
  - **T-0073 came back blocked, correctly** — my survey had classified files by
    where a type is *declared*, so `QuotaError` was swept in with the provider
    layer it happens to live in. It is not dead: it is the error currency of the
    surviving `QuotamonRunner`. Answer: move it to `Support/QuotaError.swift`,
    drop `isTransient` / `reportingPriority` / `forHTTP` (verified zero
    references), and **keep `errorDescription`** — it is the `LocalizedError`
    conformance, and `QuotaEngine.refresh` reports failures via
    `error.localizedDescription`, which routes through it; stripping it as
    "unreferenced" would have degraded every panel error to a generic string.
    Two more `Claude.providerID` / `Codex.providerID` callers surfaced in
    `SnapshotStore.swift` and `ProviderSnapshot.shortNames`.
  - **PM verification (nothing else can do it):** `xcodegen generate` +
    `xcodebuild -scheme QuotaMonitor` → **BUILD SUCCEEDED**, with
    `QuotaWidget.appex` embedded and validated in the bundle. Full suite green
    on merged master.
  - PM follow-ups taken by hand afterwards: `AGENTS.md` rewritten to describe
    the tree that now exists (the Layout entry that said "Most work lands here"
    pointed at the deleted `Providers/`); `README.md`'s QuotaKit line no longer
    claims "sources, engine (frozen)"; and **`RefreshHint: "kimi"` wired in
    `registry.go`** — T-0072 added the field but left it unset, which had
    silently dropped Kimi into the generic no-hint wording.
- **Process note:** running `xcodebuild` from inside
  `.tigerteam/worktrees/T-0073` left the shell's cwd there, so the next
  `tigerteam accept` / `status` resolved against the worktree's stale board
  copy. Accept refused ("ticket is in todo/, expected review/") rather than
  doing anything wrong, and re-running from the repo root worked. **Always `cd`
  back to the board root before any board mutation.**
- 2026-09-01 — **T-0075 accepted: the panel can update the core without a
  terminal, and the installer is genuinely pinned.** Owner asked for an in-panel
  re-fetch and assumed the download was already pinned "for security reasons".
  It was not: `fetch-quotamon.sh` pulled `releases/latest/download`, verified
  against the `SHA256SUMS` of that same release — which catches a corrupted or
  substituted download but pins no version, so the panel could install a core
  the plugin had never been tested against. Meanwhile `versionWarning` already
  detected a stale core and advised "update it and press Refresh" with **no
  button to do it** (the Install button is `visible: rows.length === 0`, so it
  hides exactly when an old-but-working core is reporting numbers).
  Landed: the script takes an optional CalVer argument, validates it against
  `^[0-9]{4}\.(1[0-2]|[1-9])\.[0-9]+$` **before** any temp dir, network call or
  write, and resolves `releases/download/v<version>`; the panel passes its own
  manifest version through argv; `Model.pinnedVersion` / `coreUpdateVersion` are
  pure and node-tested; an Update button appears only when the core is behind.
  - **Owner decision (asked, not assumed): local comparison only — no periodic
    GitHub polling.** The panel already knows the core version every refresh
    (snapshot `version`) and its own expected version (manifest), and the two
    ship from one `VERSION`, so `manifest.version > core` already means a newer
    core exists. Polling was rejected to avoid a second failure mode and a new
    trust surface in a bar widget.
  - **The two manifest fields are deliberately different, and this is the point
    the owner surfaced:** `"version"` is rewritten by `set-version.sh` (which
    anchors on `^  "version":`, two spaces, top level) and drives the *offer*;
    `dependencies.quotamon` sits four spaces deep, is touched by neither
    `set-version.sh` nor `check-versions.sh`, is hand-edited only when the
    plugin truly cannot work on an older core, and drives the red *warning*.
    So a providers-only release produces a quiet offer and no warning — the
    panel is a dumb renderer and simply draws more rows. Pinned as a test:
    manifest 2026.9.3 / min 2026.9.1 / core 2026.9.1 → offer, no warning.
  - **PM verification beyond the report:** ran the script by hand for
    `2026.9.2` (→ `releases/download/v2026.9.2`), no argument (→
    `releases/latest/download`), and eight malformed inputs — `2026.9`,
    `v2026.9.2`, `2026.13.1`, `latest`, `../../etc`, `"; id; #`, `""`, and two
    arguments — each exiting 2 with the usage line and leaving the target bin
    directory empty.
- **Consequence of T-0075:** `publish-omarchy-plugin.sh` is now load-bearing for
  user-visible behaviour, not just listing freshness. The manifest is what
  carries the news, so forgetting to publish (the T-0070 failure) now costs
  every user their update notification.
- 2026-09-01 — **T-0076 filed** (owner approved): a plugin release cut for a
  core-only change contains exactly one commit — the version bump — so
  `generate_release_notes` produces a page that says nothing about why to
  update. The workflow will pull the main repo's release notes for the same tag
  into the body. The ticket's centre of gravity is the **fallback**: the main
  release is built on a macOS runner and may not exist yet when the plugin
  publishes, so a 404 must degrade to a link, never fail the release — and the
  fetched body is text from another repository, so it must pass through verbatim
  and never be executed.
- 2026-09-01 — **T-0076 and T-0077 accepted; 2026.9.2 is cut locally and waiting
  on one owner-run command.**
  - **T-0076** — the plugin workflow now pulls the main repo's release notes for
    the same tag into the release body, falling back to a bare link when that
    release does not exist yet (the main release builds on a `macos-15` runner
    and may notarize, so the plugin can easily publish first — a 404 must never
    fail the plugin release). PM-verified by extracting the body-building step
    and running it with a deliberately hostile core body containing backticks,
    `$(id)` and a bare `---` line: all passed through verbatim, unexecuted. The
    three degraded inputs (missing file, `"body":""`, `"body":null`) each
    produced the link-only fallback. `Package` (step 34) runs before the new
    steps (51, 63), so neither temp file can reach the zip.
  - **T-0077 — a defect I specified into T-0075.** I had written "an unknown core
    version must not produce an update offer; that case is what `versionWarning`
    is for". Wrong: a core too old to report a `version` (pre-2026.9.1, before
    T-0064 added the key) still returns readings, so `rows.length === 0` hides
    the Install button, and the warning then pointed at an Update button that
    was never rendered — the exact dead end T-0075 existed to remove, relocated
    to older cores. Fixed with the asymmetry made explicit and commented: an
    **absent** version means an old release (offer), an **unparseable** one like
    `"dev"` means a deliberate local build (stay silent). The worker correctly
    identified that exactly one prior assertion had to change — the one that
    encoded the bug — and said so, as the ticket demanded.
  - **Version bump 2026.9.2 (`9cd8ac6`) exposed a release-path tripwire.**
    `model_test.mjs` pinned the live manifest version as a string literal in two
    places, so the suite failed on *any* version bump — invisible until the first
    release after the CalVer wave, which is the worst moment to find it. Fixed in
    the same commit: behaviour is asserted against a fixed manifest, and the
    shipped manifest is checked for *relationships* (`version` tracks `VERSION`,
    `minQuotamon` is valid CalVer) rather than a literal every release must chase.
  - `dependencies.quotamon` deliberately stays `>=2026.9.1`: the plugin works on
    the older core, so this ships as a quiet offer, not a red warning.
  - **PM-verified offer matrix against the shipped 2026.9.2 manifest:**
    core 2026.9.1 → button, no warning; core 2026.9.2 → neither; old core with no
    version key → button **and** warning; `dev` → neither; no snapshot → neither.
- **BLOCKED ON THE OWNER:** `scripts/release.sh` is refused by the Claude Code
  auto-mode classifier (it pushes to a remote) and must not be worked around.
  The owner runs `! bash scripts/release.sh`. Master is ~40 commits ahead of
  origin — this entire session is still local. After the main release finishes
  building, the PM runs `scripts/publish-omarchy-plugin.sh` (not blocked), which
  pushes the plugin tag and cuts a plugin release carrying the core's notes.
  The owner then updates the plugin on the Omarchy box and remounts
  (`omarchy restart shell`) before the Update button appears.
- 2026-09-01 — **2026.9.2 released and published; the Grok fix is now reachable
  from the Omarchy panel.** Owner ran `scripts/release.sh` (the PM is refused by
  the auto-mode classifier for remote pushes); master went `cd9fbb7..d61aca1`,
  ~40 commits — the whole day's work — plus tag `v2026.9.2`. The main run
  completed **success** with all six assets (universal DMG + four `quotamon`
  binaries + SHA256SUMS). The PM then ran `publish-omarchy-plugin.sh`:
  plugin master `c4196b1..7aa3d8e`, new tag `v2026.9.2`, and the plugin's own
  workflow completed **success**, cutting a release with
  `quotamon-omarchy-2026.9.2.zip`.
  - **Ordering held deliberately:** the plugin was published only after the main
    release existed, because the panel's Update button downloads from
    `releases/download/v2026.9.2/`. Verified afterwards that the exact URLs the
    panel will request return **HTTP 200**: `quotamon-linux-arm64` (6,619,296),
    `quotamon-linux-amd64` (7,217,312), `SHA256SUMS` (453).
  - Published plugin verified: manifest `version` 2026.9.2, `dependencies.quotamon`
    still `>=2026.9.1` as designed, and `Panel.qml` carries the Update button.
- **T-0076 worked, and exposed that the payload is empty.** The plugin release
  took the with-notes path — the core release's body was fetched and embedded —
  but the main repo's release body is itself only an auto-generated
  `**Full Changelog**` link, because `.github/workflows/release.yml` uses
  `generate_release_notes: true` and nobody writes notes for the core. So the
  plugin release now reads "Full Changelog" twice and still does not say
  *"adds Mistral and Together"*, which was the owner's original point. The
  mechanism is correct; the missing piece is upstream. Options if it matters:
  hand-write the main release body (edit it on GitHub, or add a CHANGELOG the
  workflow feeds to `body_path`), which T-0076 will then carry automatically.
  Not ticketed — owner's call.
- **Remaining open, owner-side:** update the plugin on the Omarchy box
  (`omarchy plugin update`, then `omarchy restart shell` — a mounted bar icon
  keeps the old instance) to get manifest 2026.9.2 and the Update button, then
  click it. Also still outstanding from the CalVer wave: the 5-second eyeball on
  the macOS About panel.
- 2026-09-01 — **T-0078 and T-0079 accepted: the installer's pin is now real
  end to end.** Both came back *blocked* rather than to review because the
  worker lanes cannot execute the installer (allowlist) or reach the network;
  both wrote correct code and refused to fake evidence, and the PM ran the
  verification.
  - **T-0078**: an explicit version argument now outranks
    `QUOTAMON_RELEASE_BASE`; setting both is refused (exit 2) so a stale
    `export` left from testing is named rather than obeyed. PM ran the full
    matrix on the branch: collision refused, override-alone honoured,
    version-alone pinned, empty override treated as unset, all eight T-0075
    rejections intact. Note: "version alone" now performs a **real** download,
    because v2026.9.2 exists — the same test 404'd during T-0075.
  - **T-0079**: the plugin ships `omarchy/quotamon-<version>.sha256` (shasum
    format, version in the filename so "pinned or not" is file existence, no
    parsing); with a version the installer verifies against that sidecar and
    **never fetches the release's `SHA256SUMS`** (attacker-controlled in the
    threat model); a version with no sidecar is refused; no version keeps
    today's behaviour with a warning. `scripts/pin-quotamon-digest.sh` writes
    the sidecar from the live release after the build; `publish-omarchy-plugin.sh`
    refuses without a pin that matches the live release; the plugin workflow
    fails the job on the same check. **The 2026.9.2 sidecar was generated by
    the PM against the live release, cross-checked byte-for-byte against
    `SHA256SUMS`, and committed on the ticket branch** (`125ecd0`) so master
    never had a state where publish would refuse for lack of a pin.
  - PM verification: pinned install (real asset, `OK`, into a temp dir);
    tamper (one hex digit flipped → `FAILED`, exit 1, nothing installed);
    `2026.9.1` with no sidecar → exit 2 before any download; unpinned latest
    warns and works; publish `--dry-run` on master reports
    `Digest pin: quotamon-2026.9.2.sha256 verified against release v2026.9.2`;
    flipped digit → `Refusing to publish: pinned digest … does not match`;
    the workflow step run by hand passes on the real sidecar and fails on a
    flipped one. Honest limit recorded in the plugin README: one GitHub account
    owns both repos, so this raises the bar and does not survive an account
    compromise.
- **INCIDENT (self-inflicted, contained): `git reset --hard` in the board
  root — the exact rule I had written into this file hours earlier.** While
  exercising the publish gate I chained `git mv -q … && commit && … ; git reset
  --hard HEAD~1`. `git mv` has no `-q`, the `&&` chain broke, and the `;`-joined
  reset ran anyway from real HEAD, dropping `209ab19 [tigerteam] board: T-0079
  -> done`. That resurrected `todo/T-0079.md`, and **`codex-1` claimed it within
  seconds** and began re-implementing a ticket merged three minutes earlier —
  field incident gh#4, reproduced by the PM. Containment, in order: `STOP.codex`;
  `git merge --ff-only 209ab19` (a pure fast-forward, no tree rewrite) to
  restore the dropped commit; global `STOP`; removed the stray `doing/` copy
  *before* killing the attempt so crash recovery had nothing to requeue; killed
  pid 26567; confirmed nothing requeued; removed the stray worktree and the
  re-created `tigerteam/T-0079` branch (0 commits beyond master); lifted both
  STOPs. Final: board clean, T-0079 only in `done/`, tree clean, suite green.
  Cost: ~90 s of one codex attempt. **Rule reinforced: never chain a
  destructive git command after `;` in the board root; test gates in a
  disposable clone, as the T-0070 worker did, not on the live board.**
- **Grok on the Mac, resolved:** the owner saw "Unrecognised response from
  Grok billing endpoint" because the running app was a local Debug build from
  11:22 bundling `quotamon 2026.9.1`, four hours before T-0071 merged. Rebuilt
  in place (`build/`, bundled core now `2026.9.2`, verified `live | ok | 0%`
  through the app's own binary). Owner must quit and relaunch.
- **Not yet published:** master carries T-0078/T-0079 (pinned installer +
  sidecar) but the plugin repo is at the T-0077 split. Publishing needs owner
  approval; the tag is already taken so it would be master-only. The 2026.9.2
  plugin release as shipped verifies against the release's own `SHA256SUMS`,
  not the sidecar — the pinned installer reaches users only after the next
  publish.
- 2026-09-01 — **Pinned installer published** (owner-approved): plugin master
  `7aa3d8e..c98d8d2`, master-only as designed — `v2026.9.2` untouched, run
  count stayed at 2 (the tag trigger correctly ignored a branch push). The
  publish gate printed `Digest pin: quotamon-2026.9.2.sha256 verified against
  release v2026.9.2` before pushing. Verified after the fact by pulling the
  **published** `fetch-quotamon.sh` and sidecar raw from GitHub into a temp dir
  and installing into a temp bin: `(pinned)`, asset only, `OK`. The security
  review's three points are now closed at the published tip: the panel path is
  pinned to a version, the digest lives in reviewed plugin code rather than
  beside the binary, and an environment override cannot outrank the pin.
  Remaining by design: one account owns both repos (signature/attestation is
  the next tier, not decided).
- 2026-09-01 — **2026.9.3 cut (owner ran `release.sh`, `4c4c640..81c76ce`,
  build success, 6 assets); T-0080 merged; plugin master published with
  `--no-tag`.** Sequence and why:
  - **Why a version-only release:** the plugin's `v2026.9.2` tag pointed at
    `7aa3d8e`, before T-0078/T-0079, so the release zip shipped the unpinned
    installer while a git clone got the pinned one. A tag never moves, and a
    plugin-only version is impossible under lockstep (the manifest version *is*
    the pinned core version, and the pin script needs a matching core release),
    so a plugin fix forces a core release. Core unchanged since 2026.9.2.
  - **The full ritual ran for the first time:** `set-version.sh` → the publish
    gate refused (`no digest pin for 2026.9.3`) → owner `release.sh` → build →
    `pin-quotamon-digest.sh` (sidecar cross-checked byte-for-byte against the
    release's `SHA256SUMS`, `32a1c0e`) → gate passed → publish.
  - **T-0080** (owner chose "refine the Omarchy card idiom" over rendering the
    console table): per-provider `CL/GPT/GK…` badges replacing the single
    speedometer glyph, percentage + countdown on the label line with the
    `resets in` line deleted (three lines per window → two), bars coloured by
    the same three severities as the bar icon (the panel previously knew only
    critical), spacing 6→4 and 10→8. 85 node assertions. QML unverifiable here;
    the diff is confined to the spec'd regions.
  - **PM error, corrected before it shipped:** I merged T-0080 saying "the tag
    is already taken, so publishing is master-only" — true of the *main* repo's
    tag, false of the *plugin's*, which `publish-omarchy-plugin.sh` creates
    from HEAD's subtree. Publishing would have tagged the untested layout.
    Fixed by adding **`--no-tag`** to the publish script (push master only;
    rerun without the flag to cut the tag), then publishing
    `c98d8d2..c134984` with it. Plugin tags remain `v2026.9.1`, `v2026.9.2`.
- **Owner's eyeball, then one command.** On the Omarchy box:
  `omarchy plugin update && omarchy restart shell`. Judge T-0080's panel.
  Specifically flagged as a taste call: **normal bars are now green**
  (`#879A39`, matching the icon) where they were foreground-grey like the Mac —
  if loud, `normal → root.foreground` in `severityColor` is a one-liner. Also
  the inter-provider gap (`Style.space(12)`) and hero padding are the next two
  height dials, not in T-0080. Once approved:
  `bash scripts/publish-omarchy-plugin.sh` (no flag) cuts plugin `v2026.9.3`
  and its release, with the pinned installer and sidecar in the zip.
- 2026-09-01 — **T-0080 rejected by the owner on a real box, T-0081 replaces
  it; on plugin master under `--no-tag` (`c134984..dd913a1`).** Owner on
  T-0080: "a total mess, all over the place, like 6 different typographics" —
  that is the card idiom itself (`display`/`body`/`caption`/bold/uppercase per
  element). Owner's new direction, superseding the earlier pick: **look like
  the console / Mac dropdown, respect the theme.** Because T-0080 only ever
  went out under `--no-tag`, no tagged release carries it.
  - **T-0081**: `Model.consoleLines/consoleText` is a JS port of
    `core/cmd/quotamon/table.go`, the same `Line`/`Span`/`Tone` model as
    `ConsoleTable.swift`, plus a fourth `dim` tone for the `░` track. PM
    verified `diff <(quotamon --demo) <(consoleText demo)` **empty**, and
    against a live snapshot identical except one-second age skew between two
    invocations. `Panel.qml`: every `Text` is `root.fontFamily` at
    `Style.font.body` (6 of 6); zero hex literals; `toneColor` is the only
    colour decision — `plain→foreground, dim→Color.muted, warning→Color.accent,
    critical→Color.urgent`; `PanelHero`/`WindowRow`/`severityColor`/badge
    deleted. `dim` was `Qt.darker(foreground,1.55)`, a fake theme colour; now
    `Color.muted`. 103 node assertions. `model_test.mjs` now skips the shared
    fixture and `../VERSION` when absent, so it is honest in the standalone
    plugin repo too.
  - **Design choice recorded:** table layout is now ported in three places
    (`table.go`, `ConsoleTable.swift`, `omarchy/Model.js`), each golden-pinned
    to `quotamon --demo`; AGENTS.md updated. The alternative — Go emitting
    spans in `--json` — was deferred: it would freeze countdowns between
    refreshes and need a core release plus a real `dependencies.quotamon` bump.
  - Known constraint, in the plugin README: the table assumes a fixed-pitch
    shell font (Omarchy's default is).
- **Owner eyeball pending:** `omarchy plugin update && omarchy restart shell`.
  If approved: `bash scripts/publish-omarchy-plugin.sh` (no flag) cuts plugin
  `v2026.9.3`. Main repo has unpushed commits (sidecar, `--no-tag`, AGENTS,
  board, STATE) — push with the owner's say-so or on the next `release.sh`.
- Observed while testing on live data: the owner's Grok token expired at
  21:10Z (its documented ~6 h lifetime); the panel now correctly says
  `Cached 37m ago — live refresh failed: Grok sign-in expired — run grok login`
  (T-0072's attached reason). Not a bug; `grok login` on that box clears it.
