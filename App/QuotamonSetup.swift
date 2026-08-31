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

/// One provider's saved state, as `quotamon config get --json` reports it.
///
/// The key itself never crosses this boundary — the core redacts it to an
/// existence flag — so this shape is safe to hold in the UI.
struct SavedProvider: Decodable, Hashable, Sendable {
    /// Whether the user has this provider switched on in the config file.
    let enabled: Bool
    /// Whether an API key is already stored for it.
    let apiKeySet: Bool

    private enum CodingKeys: String, CodingKey {
        case enabled
        case apiKeySet = "api_key_set"
    }
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
        let data = try await runner.run(["discover", "--json"], nil)
        return try JSONDecoder().decode([SetupFinding].self, from: data)
    }

    /// What the config file already says, keyed by provider id, or `nil` on a
    /// first run.
    ///
    /// `config get --json` answers with defaults merged over the file, so it
    /// cannot itself distinguish "saved as off" from "never configured"; the
    /// missing-file check is what draws that line. Callers need it because a
    /// choice the user made is not something discovery may overrule.
    func savedProviders() async throws -> [String: SavedProvider]? {
        guard await !configIsMissing() else { return nil }
        let data = try await runner.run(["config", "get", "--json"], nil)
        struct Payload: Decodable { let providers: [String: SavedProvider] }
        return try JSONDecoder().decode(Payload.self, from: data).providers
    }

    /// Path of the mandatory config file, whether or not it exists yet.
    func configPath() async throws -> String {
        let data = try await runner.run(["config", "path"], nil)
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
    /// retyping a key leaves the stored one intact. When there is a key it goes
    /// down the child's standard input under `--api-key-stdin` and never into
    /// argv: process arguments are readable by every user on the machine
    /// (`ps -ww`), which would leak the key for as long as the child lives.
    func save(id: String, enabled: Bool, apiKey: String?) async throws {
        var arguments = ["config", "set", id, "--enabled=\(enabled)"]
        var standardInput: String?
        if let apiKey, !apiKey.isEmpty {
            arguments.append("--api-key-stdin")
            standardInput = apiKey
        }
        _ = try await runner.run(arguments, standardInput)
    }
}
