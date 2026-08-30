import Foundation
import QuotaKit

/// One provider row from `quotamon discover --json`.
///
/// Field names mirror the core's JSON exactly so the app never maintains a
/// second list of what a provider is or where its credentials live.
struct SetupFinding: Decodable, Identifiable, Hashable, Sendable {
    /// Provider id as `quotamon config set` expects it, e.g. `"deepinfra"`.
    let id: String
    /// Provider name to show the user.
    let displayName: String
    /// Whether local credentials were found.
    let found: Bool
    /// Whether this build of the core can read the provider at all.
    let supported: Bool
    /// Where the core looked, and what it saw there.
    let detail: String
    /// What the user should do when nothing was found.
    let hint: String
    /// Whether the provider needs an API key typed in (DeepInfra).
    let needsKey: Bool
}

/// Drives first-run configuration through the bundled `quotamon`.
///
/// Discovery, the config path and every write go through the same executable the
/// panel reads its numbers from — the app owns no provider knowledge of its own.
/// API keys are handed straight to `config set` and are never logged or stored
/// anywhere else; the core writes the config file 0600.
struct QuotamonSetup: Sendable {

    /// The message `QuotamonRunner` raises when the core exits 3 (no config).
    ///
    /// Matched as text because `QuotaEngine` publishes failures as strings; it is
    /// a hint for showing the setup pane, never the only check.
    static let notConfiguredMessage = "Run first-time setup for quotamon"

    private let runner: QuotamonRunner

    /// Creates a setup helper over an already-resolved core runner.
    init(runner: QuotamonRunner) {
        self.runner = runner
    }

    /// Probes local credentials for every known provider. Reads files and, on
    /// macOS, the Keychain item's existence — it contacts no provider.
    func discover() async throws -> [SetupFinding] {
        let data = try await runner.run(["discover", "--json"])
        return try JSONDecoder().decode([SetupFinding].self, from: data)
    }

    /// Path of the mandatory config file, whether or not it exists yet.
    func configPath() async throws -> String {
        let data = try await runner.run(["config", "path"])
        return String(decoding: data, as: UTF8.self)
            .trimmingCharacters(in: .whitespacesAndNewlines)
    }

    /// Whether this is a first run — the core has written no config file yet.
    ///
    /// Returns false when the path cannot be resolved: an unexplained failure is
    /// not evidence of a first run, and forcing setup over live data would be
    /// worse than showing the error.
    func configIsMissing() async -> Bool {
        guard let path = try? await configPath(), !path.isEmpty else { return false }
        return !FileManager.default.fileExists(atPath: path)
    }

    /// Records one provider's choice via `quotamon config set`.
    ///
    /// `apiKey` is written only when non-empty, so re-running setup without
    /// retyping a key leaves the stored one intact.
    func save(id: String, enabled: Bool, apiKey: String?) async throws {
        var arguments = ["config", "set", id, "--enabled=\(enabled)"]
        if let apiKey, !apiKey.isEmpty {
            arguments.append("--api-key=\(apiKey)")
        }
        _ = try await runner.run(arguments)
    }
}
