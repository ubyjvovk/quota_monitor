# OpenRouter provider

The OpenRouter provider is live-only. It sends one authenticated `GET` to the
documented `https://openrouter.ai/api/v1/credits` endpoint, using the config
file's `openrouter.api_key` or the `OPENROUTER_KEY` environment fallback.

The response reports total purchased credits and total lifetime usage as USD
numbers. QuotaMon displays `max(0, total_credits - total_usage)` as the balance
and labels `total_usage` as `all time`; it never describes the lifetime total
as monthly spend. Missing or non-numeric totals make the response malformed
rather than fabricating `$0.00`.

There is no local source, quota window, token refresh, or plan field. HTTP
401/403 asks the user to replace the API key, and HTTP 429 is retryable rate
limiting. The shared fixture is documentation-derived pending live key
verification.
