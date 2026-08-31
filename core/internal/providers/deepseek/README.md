# DeepSeek provider

The DeepSeek provider is live-only. It sends one authenticated `GET` to the
documented `https://api.deepseek.com/user/balance` endpoint, using the config
file's `deepseek.api_key` or the `DEEPSEEK_KEY` environment fallback.

Money arrives as decimal strings inside `balance_infos`. QuotaMon prefers the
USD entry when present and otherwise uses the first entry. USD is displayed as
`$X.XX`; other currencies retain their code after the amount, such as
`110.00 CNY`. An empty list or unparsable `total_balance` is malformed rather
than fabricated as zero. `is_available:false` with a zero balance is a normal
account state, and the provider reports no spend because this endpoint exposes
none.

There is no local source, quota window, token refresh, or plan field. HTTP
401/403 asks the user to replace the API key, and HTTP 429 is retryable rate
limiting. The shared fixture is documentation-derived pending live key
verification.
