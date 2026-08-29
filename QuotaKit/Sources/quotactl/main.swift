import Foundation
import QuotaKit

// Diagnostic CLI: resolves every source independently and prints what each one
// returned, so a wrong number in the menu bar can be traced to its origin.
//
//   swift run quotactl            # local sources only (no network, no tokens)
//   swift run quotactl --live     # also try the live endpoints

let wantsLive = CommandLine.arguments.contains("--live")

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
            let reset = window.timeUntilReset().map { "resets in \(Self_formatDuration($0))" } ?? "reset time unknown"
            print("  \(" ".padded(14))         \(window.label.padded(8)) \(String(format: "%5.1f", used))% used   \(reset)")
        }
        if let credits = snapshot.credits {
            print("  \(" ".padded(14))         credits: balance \(credits.balance ?? "—"), unlimited \(credits.unlimited)")
        }
    case .failure(let error):
        print("  \(label.padded(14)) fail    \(error.localizedDescription)")
    }
}

func Self_formatDuration(_ interval: TimeInterval) -> String {
    let total = Int(interval)
    let hours = total / 3600, minutes = (total % 3600) / 60
    if hours >= 24 { return "\(hours / 24)d \(hours % 24)h" }
    return hours > 0 ? "\(hours)h \(minutes)m" : "\(minutes)m"
}

extension String {
    func padded(_ width: Int) -> String {
        count >= width ? self : self + String(repeating: " ", count: width - count)
    }
}

func probe(_ source: any QuotaSource) async -> Result<ProviderSnapshot, any Error> {
    do { return .success(try await source.fetch()) } catch { return .failure(error) }
}

print("QuotaKit diagnostics — live endpoints \(wantsLive ? "ENABLED" : "disabled (pass --live to try them)")\n")

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

// --debug-auth: inspect the stored Claude credential without exposing it.
// Prints key names, a short prefix and expiry only — never the token itself.
if CommandLine.arguments.contains("--debug-auth") {
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
