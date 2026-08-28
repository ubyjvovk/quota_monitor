import Foundation

public enum Claude {
    public static let providerID = "claude"
    public static let displayName = "Claude"

    /// Keychain service holding Claude Code's OAuth credentials.
    public static let keychainService = "Claude Code-credentials"

    /// Where the statusLine mirror script writes its copy of `rate_limits`.
    /// Honours `QUOTA_MONITOR_DIR`, the same override the script reads.
    public static var defaultMirrorURL: URL {
        let directory = ProcessInfo.processInfo.environment["QUOTA_MONITOR_DIR"].map(URL.init(fileURLWithPath:))
            ?? FileManager.default.homeDirectoryForCurrentUser.appendingPathComponent(".quota-monitor")
        return directory.appendingPathComponent("claude-usage.json")
    }

    /// Windows Claude reports, in the order we want them listed.
    /// Max plans carry a separate Opus-only weekly allowance.
    static let knownWindows: [(key: String, label: String, kind: QuotaWindow.Kind)] = [
        ("five_hour", "5h", .session),
        ("seven_day", "Week", .weekly),
        ("seven_day_opus", "Opus wk", .weekly),
    ]

    /// Pulls whichever known windows are present out of a `rate_limits`-shaped object.
    ///
    /// Key spellings are matched loosely because the statusLine payload
    /// (`used_percentage`) and the usage endpoint may not agree.
    static func windows(from limits: JSONValue) -> [QuotaWindow] {
        knownWindows.compactMap { spec in
            guard let node = limits.firstValue(forKey: spec.key),
                  let used = node.firstValue(forAnyKey: [
                      "used_percentage", "usedPercentage", "used_percent", "utilization",
                  ])?.double
            else { return nil }

            return QuotaWindow(
                id: spec.key,
                label: spec.label,
                kind: spec.kind,
                usedPercent: used,
                resetsAt: node.firstValue(forAnyKey: ["resets_at", "resetsAt", "reset_at"])?.date,
                windowMinutes: spec.kind == .session ? 300 : 60 * 24 * 7
            )
        }
    }

    /// Pulls the Claude OAuth details out of the Keychain blob.
    ///
    /// Scoped deliberately to the `claudeAiOauth` subtree. The same item also
    /// stores `mcpOAuth` — a map of per-MCP-server credentials that each carry
    /// their own `accessToken` — so a loose recursive search can return another
    /// service's token, or an empty one, and authenticate as the wrong thing.
    static func credentials(from json: JSONValue) throws -> (token: String, expiry: Date?, plan: String?) {
        let oauth = json["claudeAiOauth"] ?? json

        guard let token = (oauth["accessToken"] ?? oauth["access_token"])?.string,
              !token.isEmpty
        else {
            throw QuotaError.notConfigured("No Claude access token — run `claude` and sign in")
        }

        return (
            token,
            (oauth["expiresAt"] ?? oauth["expires_at"])?.date,
            (oauth["subscriptionType"] ?? oauth["subscription_type"])?.string
        )
    }

    static func snapshot(
        fromRateLimits limits: JSONValue,
        observedAt: Date,
        origin: SnapshotOrigin,
        plan: String? = nil
    ) -> ProviderSnapshot? {
        let windows = windows(from: limits)
        guard !windows.isEmpty else { return nil }
        return ProviderSnapshot(
            id: providerID,
            displayName: displayName,
            plan: plan ?? limits.firstValue(forAnyKey: [
                "subscription_type", "plan_type", "account_type",
            ])?.string,
            windows: windows,
            observedAt: observedAt,
            origin: origin
        )
    }
}

// MARK: - Local

/// Reads the file written by the Claude Code statusLine mirror script.
///
/// Claude Code hands every statusLine command a JSON payload containing
/// `rate_limits` — a documented, first-class integration point — so this needs no
/// tokens and no network. It refreshes whenever Claude Code renders.
public struct ClaudeLocalSource: QuotaSource {
    public let providerID = Claude.providerID
    public let displayName = Claude.displayName
    public let origin = SnapshotOrigin.local

    private let mirrorURL: URL

    public init(mirrorURL: URL = Claude.defaultMirrorURL) {
        self.mirrorURL = mirrorURL
    }

    public func fetch() async throws -> ProviderSnapshot {
        guard let data = try? Data(contentsOf: mirrorURL) else {
            throw QuotaError.notConfigured(
                "Statusline mirror not installed — run install-claude-statusline.sh"
            )
        }
        let root = try JSONValue.parse(data)
        let limits = root.firstValue(forKey: "rate_limits") ?? root
        let observedAt = root.firstValue(forAnyKey: ["observed_at", "observedAt"])?.date
            ?? mirrorURL.contentModifiedDate
            ?? Date()

        guard let snapshot = Claude.snapshot(
            fromRateLimits: limits,
            observedAt: observedAt,
            origin: .local,
            plan: root.firstValue(forAnyKey: ["subscription_type", "plan"])?.string
        ) else {
            throw QuotaError.noDataFound(
                "Mirror has no usage yet — Claude Code populates it after its first reply"
            )
        }
        return snapshot
    }
}

// MARK: - Live

/// Queries Anthropic's usage endpoint with Claude Code's OAuth token.
///
/// This is the same call Claude Code's own `/usage` makes. The response shape is
/// not publicly documented, so parsing is deliberately loose and any failure
/// falls back to `ClaudeLocalSource`.
public struct ClaudeLiveSource: QuotaSource {
    public let providerID = Claude.providerID
    public let displayName = Claude.displayName
    public let origin = SnapshotOrigin.live

    private let endpoint: URL
    private let session: URLSession
    private let credentialsProvider: @Sendable () throws -> Data

    public init(
        baseURL: URL = URL(string: "https://api.anthropic.com")!,
        session: URLSession = .shared,
        credentialsProvider: (@Sendable () throws -> Data)? = nil
    ) {
        self.endpoint = baseURL.appendingPathComponent("api/oauth/usage")
        self.session = session
        self.credentialsProvider = credentialsProvider
            ?? { try Keychain.genericPassword(service: Claude.keychainService) }
    }

    public func fetch() async throws -> ProviderSnapshot {
        let raw = try credentialsProvider()
        let credentials = try JSONValue.parse(raw)

        let (token, expiry, plan) = try Claude.credentials(from: credentials)

        // Claude Code refreshes this itself; if it has lapsed, using the CLI once fixes it.
        if let expiry, expiry <= Date() {
            throw QuotaError.unauthorized("Claude token expired — open Claude Code to refresh it")
        }

        var request = URLRequest(url: endpoint)
        request.timeoutInterval = 15
        request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        request.setValue("oauth-2025-04-20", forHTTPHeaderField: "anthropic-beta")
        request.setValue("application/json", forHTTPHeaderField: "Accept")
        request.setValue("2023-06-01", forHTTPHeaderField: "anthropic-version")
        // Identifies honestly. Spoofing the CLI's User-Agent was tested and made
        // no difference to the 429s, so there is nothing to gain by pretending.
        request.setValue("QuotaMonitor/0.1", forHTTPHeaderField: "User-Agent")

        let (body, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse else {
            throw QuotaError.transport("No HTTP response from \(endpoint.host() ?? "endpoint")")
        }
        guard (200..<300).contains(http.statusCode) else {
            throw QuotaError.forHTTP(http.statusCode, provider: "Claude")
        }

        let root = try JSONValue.parse(body)
        let limits = root.firstValue(forKey: "rate_limits") ?? root
        guard let snapshot = Claude.snapshot(
            fromRateLimits: limits, observedAt: Date(), origin: .live, plan: plan
        ) else {
            throw QuotaError.malformed("Unrecognised response from Claude usage endpoint")
        }
        return snapshot
    }
}
