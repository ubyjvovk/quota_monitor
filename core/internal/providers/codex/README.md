# ChatGPT / Codex provider

The provider has three quota routes. The default and preferred route runs
`codex app-server --stdio` and requests `account/rateLimits/read`; this keeps
bearer tokens inside the vendor CLI. The local fallback reads the last
`rate_limits` record from the tails of the newest
`~/.codex/sessions/**/*.jsonl` rollouts. An optional HTTP route reads the token
from `~/.codex/auth.json` only when the caller constructs it after
`QUOTA_MONITOR_CODEX_USAGE_URL` is set.

The app-server runner keeps stdin open until the `id: 2` reply arrives, then
closes stdin and reaps the process.

App-server output is newline-delimited JSON-RPC mixed with initialization
replies and notifications. The parser selects only the reply whose `id` is
`2`, then reads `result.rateLimits` by explicit path. It must not recursively
search for rate-limit keys: the sibling `rateLimitsByLimitId` subtree is a
deliberate decoy with repeated fields.

All three routes share the same normalizer, which accepts the snake-case and
camel-case window spellings and infers labels and kinds from the reported
duration. HTTP identifies itself as `quotamon`; it does not send an
`originator` header or retain the endpoint's user ID or email fields.
