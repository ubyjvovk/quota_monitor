# Provider reference — where quota actually comes from

Everything below was established by hand against real accounts on 2026-08-29,
not read from docs or guessed. It is **language-neutral on purpose**: the code
may be rewritten, but this knowledge is the expensive part and should outlive
any implementation.

Every response shape named here has a committed fixture under
`QuotaKit/Tests/QuotaKitTests/Fixtures/`. Those files are plain JSON captures
and remain valid test data for a port in any language.

---

## Claude (Anthropic) — WORKING

**Credential.** macOS Keychain, generic-password item, service
`Claude Code-credentials`.

Read it by shelling out:

```
security find-generic-password -s "Claude Code-credentials" -w
```

Do **not** use the `SecItemCopyMatching` framework API. For an item another
app owns it is either refused outright (`errSecInteractionNotAllowed` for an
unsigned binary) or answered with a GUI "allow access?" dialog, which a CLI
must never block on. Verified: the `security` CLI returns exit 0, silently.

The item is JSON. The token is at `claudeAiOauth.accessToken`.
**Address it by that explicit path.** The same blob also holds an `mcpOAuth`
map with a dozen other services' `accessToken`s, so any recursive
"find a key named accessToken" search can authenticate as the wrong service.
This bug shipped once already.

Useful sibling fields: `claudeAiOauth.expiresAt` (epoch **milliseconds**),
`subscriptionType` (e.g. `max`), `rateLimitTier`.

Treat `expiresAt` as a hint, not a verdict — Claude Code refreshes lazily and
may not have written the new value back. Send the token and let the server
decide; a stale timestamp should only shape the wording of a genuine 401.

**Endpoint.**

```
GET https://api.anthropic.com/api/oauth/usage
Authorization: Bearer <token>
anthropic-beta: oauth-2025-04-20
anthropic-version: 2023-06-01
Accept: application/json
```

Verified **200**. Fixture: `claude-usage-live.json`.

**Response.** There is **no `rate_limits` wrapper** — windows sit at the top
level, and the percentage key is `utilization`. But prefer the canonical
`limits` array, which is self-describing:

```json
"limits": [
  {"kind":"session","group":"session","percent":10,"resets_at":"…","is_active":false},
  {"kind":"weekly_all","group":"weekly","percent":14,"resets_at":"…","is_active":false},
  {"kind":"weekly_scoped","group":"weekly","percent":20,"resets_at":"…","is_active":true,
   "scope":{"model":{"display_name":"Fable"}}}
]
```

**Why this matters:** the top-level keys miss `weekly_scoped` entirely. On a
real Max account that scoped window was at **20%** — the most constrained
limit — while the top-level keys reported 14% as the worst. Parsing only the
top-level keys under-reports how close the user is to their ceiling, which is
the one thing this tool exists to get right.

Also note: `seven_day_opus` is now `null`. Any hard-coded "Opus weekly" window
is dead. Keep the top-level keys only as a fallback for the older statusline
mirror payload, which uses `rate_limits` + `used_percentage`.

**Ignore the codenamed buckets** — `tangelo`, `iguana_necktie`, `nimbus_quill`,
`omelette_promotional`, `cinder_cove`, `amber_ladder`, `juniper_tide`. They are
unreleased server-side experiments, several report `0.0`, and showing them as
real quota is wrong. The `limits` array excludes them for free.

**Extra usage** lives in `spend`:
`used.amount_minor`, `limit.amount_minor`, `limit.exponent`, `enabled`,
`disabled_reason`. **`enabled` is load-bearing.** This account shows a `20.00`
balance with `"enabled": false, "disabled_reason": "out_of_credits"` — the
credits are not spendable, and reporting "20.00 remaining" tells the user they
have headroom they do not have.

**Local fallback.** A statusline mirror script writes `rate_limits` to
`~/.quota-monitor/claude-usage.json` (override dir with `QUOTA_MONITOR_DIR`).
Older shape: `used_percentage` under a `rate_limits` wrapper. No token needed.

---

## ChatGPT / Codex — WORKING (two routes)

### Preferred: the local app-server

The Codex CLI ships a newline-delimited JSON-RPC server over stdio with a
documented `account/rateLimits/read` method. **No bearer token is handled by
us, and no bot-protected host is involved.** Measured at **0.24 s**.

```
printf '%s\n' \
  '{"method":"initialize","id":1,"params":{"clientInfo":{"name":"quota-check","title":"Quota Check","version":"0.1.0"}}}' \
  '{"method":"initialized"}' \
  '{"method":"account/rateLimits/read","id":2}' \
| codex app-server --stdio
```

Read stdout line by line and select the object whose `id` is `2`; skip the
`id: 1` reply and any `remoteControl/status/changed` notifications.

**Keep stdin OPEN until the `id: 2` line has arrived.** The server treats
stdin EOF as "client gone" and exits *before* answering; with an immediate
close you get only the `id: 1` reply and the notification (verified
2026-08-29 — an earlier revision of this file said the opposite, because the
probe that established the recipe happened to `sleep 8` before closing). Read
stdout until the `id: 2` object, then close stdin and reap the process; bound
the whole exchange with a timeout (~10 s).

Fixture: `codex-app-server-ratelimits.json`. Payload at `result.rateLimits`:

```json
{"limitId":"codex",
 "primary":  {"usedPercent":24,"windowDurationMins":300,  "resetsAt":1788038896},
 "secondary":{"usedPercent":20,"windowDurationMins":10080,"resetsAt":1788511023},
 "credits":{"hasCredits":false,"unlimited":false,"balance":"0"},
 "planType":"plus","spendControlReached":false}
```

Take `result.rateLimits` by **explicit path** — the sibling
`rateLimitsByLimitId` repeats every one of those keys, so a breadth-first
search can return the wrong subtree. `rateLimitResetCredits` also appears
(free rate-limit resets granted to the account); unused so far.

Use `windowDurationMins` to identify a window rather than assuming
`primary == 5h`.

### Alternative: HTTP

```
GET https://chatgpt.com/backend-api/wham/usage
Authorization: Bearer <~/.codex/auth.json → tokens.access_token>
ChatGPT-Account-Id: <tokens.account_id>
```

Verified **200**. Returns `plan_type`, `rate_limit.primary_window` /
`secondary_window` (`used_percent`, `limit_window_seconds`, `reset_at`),
`credits`, `spend_control`. It also returns `user_id` and `email` — do not
store those.

**Two traps here, both of which caught me:**
- `/backend-api/api/codex/usage` and `/backend-api/codex/usage` return **403
  with a Cloudflare HTML challenge**, even though the first path appears
  verbatim in the Codex binary. `wham/usage` on the *same host* returns 200.
  The path was wrong, not the host. Do not write off a host from two adjacent
  403s.
- The 403 probes sent `originator: codex_cli_rs` and a spoofed
  `User-Agent: codex_cli_rs/0.146.0`; the 200 probe sent neither. The unusual
  User-Agent is the likelier trigger. Strip odd headers before concluding
  anything.

**Policy:** identify honestly. Do not spoof a browser User-Agent or attach
cookies to get past bot protection.

### Local fallback

Codex writes a `token_count` event carrying the full `rate_limits` payload into
its session rollouts after every turn:
`~/.codex/sessions/**/rollout-*.jsonl`. Read the **tail** of the newest file —
these reach many megabytes. Snake_case shape:
`{"primary":{"used_percent":14.0,"window_minutes":300,"resets_at":1788038896},
"secondary":{…},"credits":{"has_credits":false,"unlimited":false,"balance":"0"}}`.

This is only as fresh as the user's last Codex turn. When every window has
reset since then, say so plainly — "ChatGPT reports usage only after a Codex
turn — last reading 3m ago" — rather than implying an outage.

---

## Grok (xAI) — WORKING

**Credential.** `~/.grok/auth.json`. The top level is keyed by OIDC scope: a
single key shaped `https://auth.x.ai::<client-id>`, whose value holds `key`
(the bearer JWT, **~6 hour lifetime**), `refresh_token`, `expires_at`, plus
profile fields including `email`.

Address the `https://auth.x.ai::` subtree explicitly — same hazard as Claude's
blob, several credential-shaped fields live side by side.

**Endpoint.**

```
GET https://cli-chat-proxy.grok.com/v1/billing?format=credits
Authorization: Bearer <token>
x-grok-client-mode: grok-build
Accept: application/json
```

Verified **200**. Fixture: `grok-billing-credits.json`.

Finding this took real work — record it so nobody repeats it. The host
`cli-chat-proxy.grok.com` came from `~/.grok/logs/unified.jsonl`; the
`/billing?format=credits` path and the `x-grok-client-mode` header came from
strings in the `grok` binary; the **`/v1` prefix** was the last missing piece.
Dead ends: `grok.com/rest/billing`, `/rest/billing/credits`,
`/rest/app-chat/billing`, `/rest/user/billing`, `/rest/payments/billing`,
`grok.com/api/billing`, `code.grok.com/billing`, `api.x.ai/billing`, and bare
`/billing` on the proxy host.

**Response** (under `config`):
`creditUsagePercent` (63.0), `currentPeriod.{type,start,end}` with type
`USAGE_PERIOD_TYPE_WEEKLY`, `onDemandCap`/`onDemandUsed`/`prepaidBalance` each
`{val: Number}`, `topUpMethod`, `isUnifiedBillingUser`, and:

```json
"productUsage":[{"product":"GrokBuild","usagePercent":57.0},
                {"product":"GrokImagine","usagePercent":5.0},
                {"product":"GrokChat","usagePercent":1.0}]
```

**`productUsage` is a breakdown of one shared pool** (57 + 5 + 1 ≈ 63), not
three independent allowances. Rendering each as its own quota window would tell
the user they have three separate budgets. Show the single
`creditUsagePercent` window; use the breakdown only as detail, if at all.

**Other Grok endpoints checked:**
- `GET https://api.x.ai/v1/me` → 200, identity only (`user_id`, `team_id`,
  `zdr_status`). No quota.
- `GET https://grok.com/rest/subscriptions` → 200, but it is **Stripe
  subscription history**: invoice, price and customer identifiers, no usage
  numbers. Do not ingest it.

---

## Kimi (Moonshot) — WORKING (found 2026-08-29, second attempt)

**Credential.** `~/.kimi-code/credentials/kimi-code.json` —
`access_token`, `refresh_token`, `expires_at` (epoch **seconds**),
`expires_in: 900`, `scope`, `token_type`. The token lifetime is **15
minutes**; the Kimi TUI refreshes it on launch. Base URL from
`~/.kimi-code/config.toml`: `https://api.kimi.com/coding/v1`.

**Refresh (verified 2026-08-29, captured from the TUI with the same hook):**

```
POST https://auth.kimi.com/api/oauth/token
Content-Type: application/x-www-form-urlencoded
client_id=17e5f671-d194-4dfb-9706-5516cb48c098&grant_type=refresh_token&refresh_token=<refresh_token>
→ 200 {"access_token":…,"refresh_token":…,"expires_in":900,"token_type":"Bearer","scope":"kimi-code"}
```

The `client_id` is the public Kimi Code CLI client. **The refresh token
rotates on every exchange**, so whoever refreshes must write the new pair
back to `~/.kimi-code/credentials/kimi-code.json` atomically, in the same
shape (`expires_at` = now + `expires_in`, epoch seconds), or the CLI's own
copy dies. Verified: a natively refreshed token is accepted by `/usages`
and by the CLI. `kimi -p ""` does not refresh (rejects the empty prompt
before any network call). The TUI also sends `X-Msh-Platform` /
`X-Msh-Version` / `X-Msh-Device-Id` headers; the exchange succeeded without
them, so send only an honest `User-Agent`.

**Policy (user decision 2026-08-29): quotamon does NOT refresh Kimi tokens
and does NOT run the Kimi CLI.** Rotating the refresh token from outside the
CLI is a liability the user declined. `kimi -p ""` refuses the empty prompt
before any network call, and `kimi -p "/usage"` is treated as a chat prompt
— it made three `chat/completions` calls (spent tokens) — so print mode is
out. Instead: a stale token means the CLI has not run, so nothing was spent
and the last reading is still true until a window resets → reuse the cached
reading; when even that is older than the shortest window, say "Kimi reading
is <age> old — open `kimi` to refresh its sign-in" (the TUI refreshes on
launch, no spend). The refresh contract above is recorded for reference only.

**Endpoint.**

```
GET https://api.kimi.com/coding/v1/usages        ← plural
Authorization: Bearer <access_token>
Accept: application/json
```

Verified **200**. Fixture: `kimi-usages.json` (userId redacted).

```json
{"user":{"userId":"…","region":"REGION_OVERSEA","membership":{"level":"LEVEL_BASIC"}},
 "usage":{"limit":"100","used":"3","remaining":"97","resetTime":"2026-09-04T09:42:34.674165Z"},
 "limits":[{"window":{"duration":300,"timeUnit":"TIME_UNIT_MINUTE"},
            "detail":{"limit":"100","used":"14","remaining":"86","resetTime":"2026-08-30T01:42:34.674165Z"}}],
 "parallel":{"limit":"10"},"subType":"TYPE_PURCHASE"}
```

- `usage` is the **weekly** pool (the TUI labels it "Weekly limit"; the
  reset is ~7 days out). Percent = `used / limit * 100` — note the numbers
  are **strings**.
- `limits[]` entries are extra windows described by `window.duration` +
  `window.timeUnit` (`TIME_UNIT_MINUTE`; 300 → the 5-hour window). Infer the
  label/kind from the duration, never assume.
- `membership.level` (`LEVEL_BASIC`) is the closest thing to a plan name.
- `user.userId` is an identifier — do not store it.

**How it was found** (the earlier verdict below was wrong): the TUI's
`/usage` command was driven inside `tmux` while a `NODE_OPTIONS=--require`
hook logged every `fetch`/`http.request` URL — the launcher is a Node
single-executable and honours `NODE_OPTIONS`. Ten minutes, zero guessing.
Do this first next time a CLI shows a number and the endpoint is unknown.

**Earlier dead ends (kept so nobody repeats them):** 404 on `/usage`,
`/quota`, `/balance`, `/subscription`, `/plan`, `/limits`, `/me/quota`,
`/me/usage`, `/users/me`, `/users/me/balance`, `/oauth/usage`,
`/plan/usage`. `/coding/v1/me` is identity only and returns a **phone
number** — never call it. The `/usage` strings in the binary are bundler
paths, not routes.

---

## DeepInfra — WORKING, but it is spend, not quota

**Credential.** API key. Read it from the **environment** (`DEEPINFRA_KEY`).
The repo's `<root>/.env` holds a copy, but it is deliberately masked from
sandboxed workers — code must not learn to parse it.

Endpoints (from `https://docs.deepinfra.com/llms.txt`; note the paths are
**not** under `/v1`):

```
GET https://api.deepinfra.com/payment/config
    → {"limit": -1.0}          # USD spending limit; negative means no limit

GET https://api.deepinfra.com/payment/usage?from=current
    → {"months":[{"period":"2026.08",
                  "interval":{"fr":…,"to":…},   # epoch MILLISECONDS
                  "items":[…],
                  "total_cost":775,             # CENTS
                  "invoice_id":"INVALID"}],
       "initial_month":"2026.08"}
```

Both verified **200**. Fixture: `deepinfra-usage.json` (trimmed to two items;
the live response lists every model the account used).

**Balance (found 2026-08-29):**

```
GET https://api.deepinfra.com/payment/checklist
→ {"stripe_balance": -18.0, "recent": 7.97, "limit": null, "suspended": false,
   "overdue_invoices": 0.0, "billing_type": "balance", "topup": false, …}
```

Verified **200**. `stripe_balance` is USD; **negative = funds on account,
positive = money owed** (from the OpenAPI description). `recent` is usage
not yet invoiced against that balance, in USD. **The remaining balance the
dashboard shows is `|stripe_balance| − recent`** — on this account 18.00 −
7.97 = 10.03, matching the dashboard's "$10.03 prepaid credits" (user
verified 2026-08-29). Don't present `stripe_balance` alone as "remaining". `limit` the spending limit (null = none), `suspended` /
`suspend_reason` / `overdue_invoices` are actionable status. Fixture:
`deepinfra-checklist.json` (money fields only).

**PII warning:** the same response carries `billing_address_info` (name,
street address) and `payment_method_info`. Take exactly the fields above and
nothing else; never log or persist the raw body.

`GET https://api.deepinfra.com/v1/me` also returns 200 but is identity and
account flags only — no balance. 404 on `/v1/billing`, `/dash/billing`,
`/dash/balance`, `/dash/usage`, `/v1/credits`, `/v1/me/usage`, `/v1/account`,
`deepinfra.com/api/billing`. `/v1/inference/usage` exists but rejects GET.

**DeepInfra is pay-as-you-go: there is no quota, but there IS a prepaid balance (see above).** The
honest readouts are month-to-date spend (`total_cost / 100` USD) and, when
`limit > 0`, usage against that limit. On this account `limit` is `-1`, so
there is no percentage to show — only "$7.75 this month". Decide deliberately
how to present a provider that has spend but no ceiling; do not invent a
percentage.

---

## Replicate — EXCLUDED (no billing via the API token)

**Credential.** API token in the environment (`REPLICATE_KEY`). It authorises
the public API only.

**Verdict:** the public API (`api.replicate.com/v1/...`) has **no** billing,
spend, credit, usage, invoice, or limit endpoint. `GET /v1/account` returns
identity only (`username`, `name`, `avatar_url`, `github_url`, `type`) — no
money. 404 on `/v1/billing`, `/v1/usage`, `/v1/spend`, `/v1/credits`,
`/v1/invoices`, `/v1/limits`, `/v1/account/{billing,usage,credits,limits}`,
and the same under `replicate.com/api/...`. The docs' HTTP reference lists no
billing path.

The dashboard number (`replicate.com/api/users/<user>/unused-credit`) is
**cookie-gated**: it is authorised by the browser login session, not the API
token. With `Authorization: Bearer <REPLICATE_KEY>` it returns **403 "You do
not have permission"**. Reaching it would mean reusing the browser session
cookie — barred by rule 7 below (no cookies lifted from a browser). The only
quota-like API signal is the short-window `ratelimit-remaining` / `ratelimit-reset`
response headers (~3000/second), which are an API request rate limit, not
subscription spend, and are useless as a quota reading.

**Decision (user, 2026-08-30): do not add Replicate.** Revisit only if
Replicate ships a documented spend/credit endpoint reachable with the API
token. Do not re-chase the dashboard route.

---

## Cross-cutting rules learned the hard way

1. **A window that has reset reports no reading, never `0%`.** Zero reads as
   "plenty left"; the truth is "we don't know yet". Render `—`.
2. **Timestamps are inconsistent.** Claude's `expiresAt` and DeepInfra's
   `interval` are epoch **milliseconds**; Codex `resetsAt` is **seconds**;
   Claude's `resets_at` and Grok's period bounds are **ISO-8601**. Coerce
   defensively (a value past ~year 2286 in seconds is really milliseconds).
3. **Never search a credential blob recursively.** Claude's Keychain item and
   Grok's auth file both hold several services' tokens. Address by explicit
   path, always.
4. **Do not ingest PII or billing identifiers.** Kimi `/me` returns a phone
   number; Grok `/rest/subscriptions` returns Stripe IDs; ChatGPT
   `wham/usage` returns an email. None of it is quota.
5. **Report failures the user can act on.** "Run `grok login`" beats a status
   code. Rank an expired token above an unconfigured optional source, or you
   send the user to fix the wrong thing.
6. **Prefer a vendor's local CLI over its private HTTP API** where one exists
   (`security` for Claude, `codex app-server` for ChatGPT). No token handling,
   no bot protection, and the vendor maintains the contract.
7. **Identify honestly.** No spoofed User-Agents, no cookies lifted from a
   browser, no bot-protection workarounds.
