import Foundation
import QuotaKit

// Console front-end: resolved quota for every configured provider, one line
// per window. `--diagnose` keeps the original per-source probe for tracing a
// wrong number back to the source that produced it.

let args = Array(CommandLine.arguments.dropFirst())

let usage = """
usage: quotactl [--json] [--no-live] [--diagnose] [--debug-auth] [--help|-h]

Print the resolved quota for every configured provider.

  --json         same data as pretty-printed JSON
  --no-live      skip live endpoints; local sources only
  --diagnose     probe each source independently
  --debug-auth   inspect the stored Claude credential (never prints the token)
  --help, -h     show this help and exit
"""

if args.contains("--help") || args.contains("-h") {
    print(usage)
    exit(0)
}

let known: Set<String> = ["--json", "--no-live", "--diagnose", "--debug-auth", "--help", "-h"]
let unknown = args.filter { $0.hasPrefix("-") && !known.contains($0) }
if let flag = unknown.first {
    fputs("unrecognised flag: \(flag)\n\(usage)\n", stderr)
    exit(2)
}

let wantsJSON = args.contains("--json")
let noLive = args.contains("--no-live")
let diagnose = args.contains("--diagnose")
let debugAuth = args.contains("--debug-auth")

if diagnose {
    await runDiagnostics(wantsLive: !noLive)
} else {
    let code = await runResolved(json: wantsJSON, liveEnabled: !noLive)
    if debugAuth { printDebugAuth() }
    exit(Int32(code))
}

if debugAuth { printDebugAuth() }

// MARK: - Resolved view

func runResolved(json: Bool, liveEnabled: Bool) async -> Int {
    let providers = ProviderCatalog.all(isLiveEnabled: { _ in liveEnabled })
    let snapshots = await fetchAll(providers)
    let asOf = Date()

    if json {
        do {
            print(try ConsoleReport.renderJSON(snapshots, asOf: asOf))
        } catch {
            fputs("failed to encode JSON: \(error.localizedDescription)\n", stderr)
            return 1
        }
    } else {
        let text = ConsoleReport.render(snapshots, asOf: asOf)
        if !text.isEmpty {
            print(text, terminator: "")
        }
    }

    return snapshots.contains(where: { !$0.windows.isEmpty }) ? 0 : 1
}

func fetchAll(_ providers: [HybridProvider]) async -> [ProviderSnapshot] {
    await withTaskGroup(of: (String, ProviderSnapshot).self) { group in
        for provider in providers {
            group.addTask { await (provider.providerID, provider.fetch()) }
        }
        var byID: [String: ProviderSnapshot] = [:]
        for await (id, snapshot) in group {
            byID[id] = snapshot
        }
        return providers.map { byID[$0.providerID]! }
    }
}

// MARK: - Diagnose (legacy per-source probe)

func runDiagnostics(wantsLive: Bool) async {
    func describe(_ label: String, _ result: Result<ProviderSnapshot, any Error>) {
        switch result {
        case .success(let snapshot):
            let age = Int(snapshot.age())
            print("  \(label.padded(14)) ok      \(snapshot.displayName) [\(snapshot.plan ?? "—")]  observed \(age)s ago")
            for window in snapshot.sortedWindows() {
                // Matches the app: a reset window has no reading, rather than zero.
                guard let used = window.currentUsedPercent() else {
                    print("  \(" ".padded(14))         \(window.label.padded(8))     — no reading since this window reset")
                    continue
                }
                let reset = window.timeUntilReset().map { "resets in \(QuotaFormat.countdown($0))" } ?? "reset time unknown"
                print("  \(" ".padded(14))         \(window.label.padded(8)) \(String(format: "%5.1f", used))% used   \(reset)")
            }
            if let credits = snapshot.credits {
                print("  \(" ".padded(14))         credits: balance \(credits.balance ?? "—"), unlimited \(credits.unlimited)")
            }
        case .failure(let error):
            print("  \(label.padded(14)) fail    \(error.localizedDescription)")
        }
    }

    func probe(_ source: any QuotaSource) async -> Result<ProviderSnapshot, any Error> {
        do { return .success(try await source.fetch()) } catch { return .failure(error) }
    }

    print("QuotaKit diagnostics — live endpoints \(wantsLive ? "ENABLED" : "disabled")\n")

    print("Claude")
    describe("local/mirror", await probe(ClaudeLocalSource()))
    if wantsLive { describe("live/oauth", await probe(ClaudeLiveSource())) }

    print("\nChatGPT (Codex)")
    describe("local/rollout", await probe(CodexLocalSource()))
    if wantsLive { describe("live/backend", await probe(CodexLiveSource())) }

    let store = SnapshotStore()
    print("\nSnapshot store")
    print("  path            \(store.fileURL.path)")
    print("  widget-readable \(store.isSharedWithWidget ? "yes (App Group)" : "no — needs an App Group entitlement")")
}

extension String {
    func padded(_ width: Int) -> String {
        count >= width ? self : self + String(repeating: " ", count: width - count)
    }
}

// --debug-auth: inspect the stored Claude credential without exposing it.
// Prints key names, a short prefix and expiry only — never the token itself.
func printDebugAuth() {
    print("\nClaude credential")
    do {
        let raw = try Keychain.genericPassword(service: Claude.keychainService)
        let json = try JSONValue.parse(raw)

        if case .object(let top) = json {
            print("  top-level keys   \(top.keys.sorted().joined(separator: ", "))")
            for (name, value) in top {
                if case .object(let inner) = value {
                    print("  \(name) keys  \(inner.keys.sorted().joined(separator: ", "))")
                }
            }
        }

        let oauth = json["claudeAiOauth"] ?? json
        if let token = (oauth["accessToken"] ?? oauth["access_token"])?.string, !token.isEmpty {
            print("  token            present (\(token.count) chars)")
        } else {
            print("  token            NOT FOUND")
        }

        if let expiry = (oauth["expiresAt"] ?? oauth["expires_at"])?.date {
            let expired = expiry <= Date()
            print("  expiresAt        \(expiry) — \(expired ? "EXPIRED" : "valid")")
        } else {
            print("  expiresAt        not present (expiry check is being skipped)")
        }

        for key in ["scopes", "scope", "subscriptionType", "subscription_type"] {
            if let value = oauth[key] ?? json.firstValue(forKey: key) {
                print("  \(key)".padded(19) + "\(value.string ?? String(describing: value))")
            }
        }
    } catch {
        print("  unreadable: \(error.localizedDescription)")
    }
}
