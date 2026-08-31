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

The local scan is deliberately forgiving. A rollout it cannot read — permissions,
a file rotated away mid-scan — is skipped, and its error is only reported if no
rollout at all yields a reading; one broken session must not blind the source.
Within a file, lines containing `rate_limits` are offered newest-first until one
parses as a real `payload.rate_limits` record, because a transcript's last mention
of the phrase is often just chat text quoting it. Each pass offers at most 32
candidates; the tail is read first as a fast path, and the whole file is re-read
only when every tail candidate is rejected.

App-server output is newline-delimited JSON-RPC mixed with initialization
replies and notifications. The parser selects only the reply whose `id` is
`2`, then reads `result.rateLimits` by explicit path. It must not recursively
search for rate-limit keys: the sibling `rateLimitsByLimitId` subtree is a
deliberate decoy with repeated fields.

All three routes share the same normalizer, which accepts the snake-case and
camel-case window spellings and infers labels and kinds from the reported
duration. HTTP identifies itself as `quotamon`; it does not send an
`originator` header or retain the endpoint's user ID or email fields. The HTTP
route also renames the endpoint's `reset_at` to the normaliser's `resets_at`;
passing the endpoint's own spelling through drops every live reset time.
