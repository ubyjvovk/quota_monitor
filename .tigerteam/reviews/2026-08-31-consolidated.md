# Stress review — PM consolidation (2026-08-31)

Three independent reviews (opus 15, codex 15, ds 9 findings; kimi lane out of
quota, parked). PM spot-verified every "certain" claim used below.

## Consensus (2–3 reviewers agree) → tickets
- Exit semantics: valid credits-only / no-current-window snapshots exit 1 and
  frontends discard the JSON (opus F1, codex F5, ds F3 variant) → **T-0045**
- Hybrid cache policy: cache ignored on live outage; stale-token gate freezes
  Kimi ≤5h and Grok ≤7d; provider hint overwritten (opus F7, ds F1/F2/F8, ds F7) → **T-0047**
- Secrets: `config get --json` prints api_key; key in argv (opus F2, codex F2) → **T-0048** (core), **T-0054** (app)
- Setup overwrites unreadable config, losing keys (opus F5, codex F1) → **T-0049**
- Runner 15 s kill < core's 25+15 s Kimi worst case (opus F3, codex F3) → **T-0054**
- Waybar drift: credits-only-if-windowless, raw order, raw origin, >100% (opus F11, codex F13/F14, ds F4/F9) → **T-0046**
- History records 0% for rolled-over windows (opus F6, codex F15) → **T-0053**
- Short-name drift GK/DI/KM vs GR/DE/KI (opus F13, ds F5) → **T-0053**
- SHA256SUMS self-entry; build.sh masks failures (opus F15, codex F10, codex F9) → **T-0055**
- Omarchy stuck-fetch watchdog (codex F12; opus noted unfiled) → **T-0056**
- Condensed widget hides credits-only providers and staleness (codex F8, opus F10) → **T-0054**

## Verified singletons → tickets
- codex http `reset_at` vs `resets_at` key mismatch (opus F4) → **T-0050**
- codex local: per-file abort; substring match skips real record (opus F9) → **T-0050**
- app-server test 3 s budget flakes under load (T-0041 incident) → **T-0050**
- DeepInfra `$%d` invoice count; missing money fields become $0.00 ok (opus F8, codex F4) → **T-0051**
- Grok hard-coded "Week"; discover $PATH `security`; scope map order (opus F12, F14) → **T-0052**
- SetupView forgets saved enabled state (codex F7) → **T-0054**
- `--fresh` usage text vs rejection (ds F6) → **T-0045**

## Rejected / deferred
- codex F6 (Kimi CLI launch "violates policy"): **rejected** — the pty launch
  IS the sanctioned mechanism; the policy forbids rotating tokens ourselves.
- codex F11 (frozen Swift Codex live headers/shape): deferred — the Go core is
  the product; the Swift fetchers are a frozen reference.
- Full origin column in condensed widget rows: T-0054 ships a staleness
  marker instead; a column does not fit 17 cells.
- Omarchy multi-monitor dedup: already a documented limitation.
