# Grok provider

The live source reads the Grok CLI bearer token from `~/.grok/auth.json`. The
file is keyed by OIDC scope, so QuotaMon deterministically selects the first
key beginning with `https://auth.x.ai::` and reads only that object's `key` and
`expires_at` fields. It does not ingest the neighboring email or profile data.

Usage comes from
`GET https://cli-chat-proxy.grok.com/v1/billing?format=credits` with the
`x-grok-client-mode: grok-build` header. The response's `productUsage` entries
are a breakdown of one shared allowance, not independent quotas. QuotaMon
therefore exposes exactly one weekly window from `config.creditUsagePercent`
and never turns product breakdowns into additional windows.
