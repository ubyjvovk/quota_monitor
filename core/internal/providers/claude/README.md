# Claude provider

The live source reads Claude Code's OAuth credential JSON. On macOS it runs
`/usr/bin/security find-generic-password -s "Claude Code-credentials" -w`;
on Linux, Windows, and other platforms it reads
`~/.claude/.credentials.json`. Credential fields are addressed only through
`claudeAiOauth` (with a legacy top-level fallback), because the same blob can
contain unrelated MCP access tokens.

Live usage comes from `GET https://api.anthropic.com/api/oauth/usage` using the
Claude OAuth token. The parser prefers the response's self-describing `limits`
array, which includes scoped weekly limits absent from legacy top-level fields.
Only session, `weekly_all`, and `weekly_scoped` entries are exposed, so
codenamed experimental buckets are not mistaken for user quota. If no usable
`limits` array exists, the parser falls back to the statusline mirror's direct
`five_hour` and `seven_day` nodes.

The local source reads `claude-usage.json` from `QUOTA_MONITOR_DIR`, or from
`~/.quota-monitor` when the override is unset. It needs no credential or
network access.
