# Kimi provider

The live source reads the Kimi CLI bearer token from
`~/.kimi-code/credentials/kimi-code.json` — the `access_token` field, addressed
by explicit path. The token lives only **15 minutes** and the Kimi TUI refreshes
it on launch; when it has lapsed the source reports
"Kimi sign-in expired — run `kimi` once" rather than guessing at a refresh
endpoint (the refresh route is not yet known).

Usage comes from

```
GET https://api.kimi.com/coding/v1/usages        ← note the plural
Authorization: Bearer <access_token>
Accept: application/json
```

The singular `/usage` and a dozen other guessed routes are 404, and `/me`
returns identity data including a phone number that must never be ingested — so
QuotaMon only ever addresses the response's `user`, `usage`, and `limits`
subtrees. The top-level `usage` is the weekly pool (Kimi reports the numbers as
**strings**, so QuotaMon parses `used`/`limit` before computing a percentage);
each `limits[]` entry is an extra window described by `window.duration` +
`window.timeUnit`. A limit of `0` or a missing/unparsable one is skipped, never
turned into a fabricated reading or a divide-by-zero. `membership.level`
(`LEVEL_…` stripped and lower-cased) supplies the plan, and `user.userId` is
never copied anywhere.
