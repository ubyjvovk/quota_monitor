# DeepInfra provider

DeepInfra is pay-as-you-go: there is **no quota and no prepaid balance**. The
honest readout is month-to-date spend, and a percentage exists **only** when
the account has a spending limit set. QuotaMon never invents a ceiling.

The API key is read from the **`DEEPINFRA_KEY` environment variable only**. The
repo's `.env` holds a copy, but it is deliberately masked from sandboxed
workers and the code must never learn to parse it.

Spend comes from the DeepInfra payment API; the paths are **not** under `/v1`:

- `GET /payment/config` returns the USD spending limit (`limit <= 0` means no
  limit).
- `GET /payment/usage?from=current` returns `months[0]` with `total_cost` in
  **cents** and `interval.to` in **epoch milliseconds**.

QuotaMon summarises this as a `monthly_spend` window (`spent / limit * 100`)
only when a positive limit exists; otherwise it reports just
"$X.XX this month" as spend with no percentage, per the cross-cutting rule
that a provider without a ceiling has no percentage to show.
