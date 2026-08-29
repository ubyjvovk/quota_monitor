# Kimi provider

The live source reads the Kimi CLI bearer token from
`~/.kimi-code/credentials/kimi-code.json` — the `access_token` field, addressed
by explicit path. The token lives only **15 minutes** and the Kimi TUI refreshes
it on launch. When a cached reading is no longer young enough to reuse,
QuotaMon launches the TUI through `script(1)`, waits for startup, and sends
`/exit`; it then accepts the refresh only if the CLI-owned credential file has
a future `expires_at`. QuotaMon never reads the refresh token, rotates it, or
calls the authentication endpoint itself.

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
