# Agent orientation

<!-- Maintained by the Tiger Team PM. Workers and agent CLIs read this first;
     workers never edit it. -->

## What this project is
QuotaKit shows how much of your LLM subscription quota you have left — Claude
(Anthropic) and ChatGPT (Codex). Each provider has a **local** source (a file
some CLI already writes) and a **live** source (an authenticated HTTP endpoint);
`HybridProvider` runs both and prefers live, falling back to the cached local
reading with an explanatory status. Everything normalises to `ProviderSnapshot`
→ `[QuotaWindow]` with a `usedPercent` and a `resetsAt`.

**Current focus is the Go core port (`core/`) — read `GO-PORT.md`, then
`PROVIDERS.md`.** The Swift `QuotaKit` fetchers are frozen (bug-fix only) and
serve as the reference semantics; `quotactl` is the parity oracle. The macOS
menu-bar app and widget are ON HOLD.

## Layout
- `QuotaKit/Sources/QuotaKit/Providers/` — per-provider sources. **Most work lands here.**
- `QuotaKit/Sources/QuotaKit/Models/` — `ProviderSnapshot`, `QuotaWindow`, `UsagePace`.
- `QuotaKit/Sources/QuotaKit/Support/` — `JSONValue` (loose JSON), `Keychain`, `QuotaFormat`.
- `QuotaKit/Sources/QuotaKit/Engine/` — refresh loop + snapshot/history persistence.
- `QuotaKit/Sources/QuotaKit/UI/` — SwiftUI helpers. **On hold, do not edit.**
- `QuotaKit/Sources/quotactl/main.swift` — the console tool.
- `QuotaKit/Tests/QuotaKitTests/` — the whole suite, plus `Fixtures/`.
- `App/`, `Widget/`, `QuotaMonitor.xcodeproj/`, `project.yml` — the macOS
  menu-bar app + widget. **Editable only on the `opus` lane** (runs outside
  Seatbelt, so it can `xcodebuild`); sandboxed lanes cannot build these and
  must not touch them. Tickets that edit here carry `capability: [macbuild]`
  and `assignee: opus`.
- `QuotaKit/.build/` — generated. Never edit, never commit.

## Go core (`core/`)
- Module `quotamon`, Go 1.27, **stdlib only, no cgo**. `GOPROXY=off` in the
  sandbox — a `go get` fails. `go vet` runs as part of the test command.
- Layout: `cmd/quotamon/` (CLI), `internal/snapshot/` (model + JSON contract),
  `internal/source/` (Source interface, Error kinds = reporting priority),
  `internal/jsonx/` (explicit-path JSON helpers — **no recursive key search**),
  `internal/providers/<name>/`, `internal/fixtures/` (loads the shared Swift
  fixtures by relative path).
- JSON contract: timestamps RFC 3339 UTC with **no fractional seconds**
  (Swift's decoder rejects them); `windows` is always an array; status is
  `{"state":"ok"|"needsSetup"|"failed","message"?}`.
- Anything that execs (`security`, `codex`) or does HTTP sits behind a func
  field / `*http.Client` with a working default; tests inject `httptest` or a
  stub and never touch `$HOME`, the Keychain, the network, or a subprocess.
- Table-driven tests, `t.Run` per case, names that read as sentences.
- Providers live in `internal/providers/<id>/` with a `README.md` each; the
  registry (`internal/registry/`) fixes display order. Adding a provider =
  new package + one registry line + a short name in `cmd/quotamon/waybar.go`.
- Hand-check any provider change with `cd core && go run ./cmd/quotamon check`
  (probes every source independently) and compare against
  `swift run --disable-sandbox --package-path QuotaKit quotactl --json` — the
  Swift fetchers are the frozen reference until the cutover.
- Docs map: `PROVIDERS.md` = the provider contract (endpoints, credentials,
  gotchas — update it when you learn something new about an endpoint);
  `GO-PORT.md` = design record + status table; `core/README.md` = user-facing
  usage; this file = how to work here.

## Conventions
- Swift 6 with strict concurrency. Anything crossing an `async` boundary is
  `Sendable`; sources are `struct`s conforming to `QuotaSource`.
- Tests use **swift-testing**, not XCTest: `@Test func name() async throws`,
  `#expect(...)`, `#require(...)`. Test names are sentences describing the
  behaviour (`cachedReadingIsKeptAndLabelledWhenLiveFails`), not `testFoo`.
- **Dependencies are injected through the initialiser with a working default.**
  `CodexLocalSource(home:)`, `ClaudeLiveSource(baseURL:session:credentialsProvider:)`.
  This is how anything touching the network, the Keychain, or `$HOME` gets
  tested — follow it for every new source.
- Comments explain *why*, not *what*. Several comments in this codebase record a
  real incident; do not delete them when editing nearby code.
- Errors are `QuotaError` cases whose message tells the user what to *do*
  ("run `claude` in a terminal to sign in again"), never a raw status code.

## Config
- Project config is root `tigerteam.toml` (machine-local facts in `~/.tigerteam.toml`).
- The runner injects `TIGERTEAM_TEST_CMD` into every worker environment;
  `run-tests.sh` uses that first, then `tigerteam config get test_cmd`.

## Commands
- Tests: `bash .tigerteam/scripts/run-tests.sh` — the ONLY way to run tests.
  It runs `scripts/test-all.sh`: the Swift suite, then `go vet` + `go test`
  in `core/` once `core/go.mod` exists.
- macOS app (opus lane only, outside Seatbelt): build + headless render:
  `xcodegen generate && xcodebuild -project QuotaMonitor.xcodeproj -scheme QuotaMonitor -configuration Debug -derivedDataPath .build/xcode build`
  then render screenshots without a display via the app's `QUOTA_MONITOR_RENDER`
  path (see `App/DesignSnapshot.swift`). `run-tests.sh` (swift+go) is still the
  shared gate; the app build is an extra check the opus worker runs itself and
  the PM re-runs at review.
- Run the console tool by hand:
  `swift run --disable-sandbox --package-path QuotaKit quotactl`

## Landmarks & gotchas
1. **You run inside a Seatbelt sandbox, and it is strict.** `$HOME` is
   unreadable apart from a few carved-out dirs; the project root is READ-ONLY
   outside your worktree; `<root>/.env` is masked. Write only inside your
   worktree.
2. **`swift` needs `--disable-sandbox` here.** SwiftPM evaluates `Package.swift`
   in its own nested `sandbox-exec`, and macOS refuses to nest sandboxes — without
   the flag you get `sandbox_apply: Operation not permitted`. `test_cmd` already
   has it; add it to any `swift` command you run by hand.
3. **Never make a test touch the real Keychain, the real network, or the real
   `$HOME`.** Those all fail or hang under the sandbox, and would be flaky
   outside it. Inject a stub (see Conventions). A test that shells out to
   `security` or hits `api.anthropic.com` will be rejected.
4. `JSONValue.firstValue(forKey:)` searches **breadth-first through the whole
   document** and returns an arbitrary match when a key repeats. It is fine for
   usage payloads, and actively dangerous for credential blobs — the Claude
   Keychain item holds `claudeAiOauth` *and* a `mcpOAuth` map with a dozen other
   services' `accessToken`s. Address credentials by explicit path. This bug
   shipped once already; see the comment on `Claude.credentials(from:)`.
5. A quota window that has reset since it was recorded reports **no reading**
   (`nil`), never `0%`. `currentUsedPercent()` enforces this; UI and CLI must
   render `—` rather than a zero the user would misread as "plenty left".
6. Fixtures live in `Tests/QuotaKitTests/Fixtures/` and are loaded via
   `Bundle.module`; new fixture files need no `Package.swift` change (the whole
   directory is copied).
