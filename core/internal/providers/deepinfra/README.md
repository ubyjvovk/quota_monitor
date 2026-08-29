# DeepInfra provider

DeepInfra is pay-as-you-go: there is **no quota window beyond an optional
spending limit**, and a percentage exists **only** when the account has that
limit set. The account does carry a **prepaid balance** from
`/payment/checklist`, which QuotaMon shows as the headline next to
month-to-date spend. QuotaMon never invents a ceiling.

The API key is read from the **`DEEPINFRA_KEY` environment variable only** (or
the config file's `deepinfra.api_key`). The repo's `.env` holds a copy, but it
is deliberately masked from sandboxed workers and the code must never learn to
parse it.

Data comes from the DeepInfra payment API; the paths are **not** under `/v1`:

- `GET /payment/config` returns the USD spending limit (`limit <= 0` means no
  limit).
- `GET /payment/usage?from=current` returns `months[0]` with `total_cost` in
  **cents** and `interval.to` in **epoch milliseconds** — this is the Spend
  reading ("$X.XX this month").
- `GET /payment/checklist` returns the prepaid balance and account status:
  `stripe_balance` (**negative = funds ready to spend, positive = money owed**,
  by the vendor's OpenAPI description), `suspended` / `suspend_reason`,
  `overdue_invoices`, and more.

**PII rule:** the checklist response also carries `billing_address_info`
(here the user's postal address) and `payment_method_info`. Address only the
money/status fields (`stripe_balance`, `suspended`, `suspend_reason`,
`overdue_invoices`, `recent`, `limit`, `billing_type`, `topup`) and never
retain the body. The encoded snapshot must never contain the address.

QuotaMon fetches all three endpoints concurrently. Mapping:

- `stripe_balance < 0` → spendable balance `"$X.XX"`, `HasCredits: true`,
  `Enabled: !suspended`.
- `stripe_balance == 0` → `"$0.00"`, `HasCredits: false`.
- `stripe_balance > 0` → `"$X.XX owed"`, `HasCredits: false`.
- Checklist failed but usage succeeded → the spend-only fallback with a
  "Balance unavailable" `NeedsSetup` status, so a balance outage never hides
  month-to-date spend.
- `Spend` is always `"$X.XX this month"` from `/payment/usage` when usage
  succeeded.
- `suspended` → `Failed` status; `overdue_invoices > 0` → `NeedsSetup`.

A `monthly_spend` percentage window (`spent / limit * 100`) appears only when
a positive limit exists; otherwise the provider reports spend and balance with
no percentage, per the cross-cutting rule that a provider without a ceiling
has no percentage to show.
