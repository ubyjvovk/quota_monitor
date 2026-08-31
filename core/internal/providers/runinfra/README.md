# RunInfra provider

RunInfra (runinfra.ai) is **live-only**: there is no local file source. QuotaMon
reads prepaid credits and month-to-date spend from its credits API, mirroring
the DeepInfra wire-up (config key + `RUNINFRA_TOKEN` fallback).

The API key is read from the config file's `runinfra.api_key`, falling back to
the **`RUNINFRA_TOKEN` environment variable**. The repo's `.env` is deliberately
masked from sandboxed workers and the code must never learn to parse it.

Data comes from `GET https://api.runinfra.ai/v1/credits` with
`Authorization: Bearer <key>`. All money is in **US cents, integers**. The
relevant fields:

- `available_cents` — the headroom admission checks (the dashboard's spendable
  balance). This is the **Balance** headline, **never** the ledger
  `balance_cents` (which includes held funds and can overstate spendable
  headroom). `held_cents` is transient and ignored.
- `period.spent_cents` — month-to-date spend, shown as "$X.XX this month".
- `spend_cap.limit_cents` / `spend_cap.used_cents` / `spend_cap.hard` — an
  optional monthly spend cap. `hard:false` means alert-only.
- `plan_tier` — the subscription tier, omitted from the snapshot when empty.
- `as_of` — when the numbers were true, with **fractional seconds + Z**; the
  parser accepts them and our emitted snapshot stays fractional-free.

Mapping:

- Credits: `Balance = available_cents` as `$X.XX`, `Spend =
  period.spent_cents` as `$X.XX this month`, `Enabled true`, `Unlimited false`,
  `HasCredits = available_cents > 0`.
- A `monthly_cap` percentage window (`used / limit * 100`, label `Cap`, monthly
  kind, no reset — the API gives no period end) appears **only** when
  `spend_cap.limit_cents` is present, positive, and `hard: true`. A soft cap
  (`hard:false`) or an absent cap yields **no window**: `gates_inference` is
  false, so a soft cap is advisory and must not read as quota pressure.
- Missing or malformed `available_cents` or `period.spent_cents` makes the
  response malformed — it is **never** treated as zero money (T-0051 rule).
- 401/403 → the API key is rejected (NeedsSetup with a setup action); 429 is
  rate limited (60 reads/min per key); other non-200 → Transport.

RunInfra has no `TokenStale`/`Refresh` policy: it is a plain API key. The live
reading is cached like DeepInfra.
