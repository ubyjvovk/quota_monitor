import Foundation

public enum Codex {
    public static let providerID = "codex"
    public static let displayName = "ChatGPT"

    public static var defaultHome: URL {
        FileManager.default.homeDirectoryForCurrentUser.appendingPathComponent(".codex")
    }

    /// The ChatGPT backend sits behind bot protection and refuses plain HTTP
    /// clients, so no live source is wired up unless the user points us at a
    /// reachable endpoint themselves. Quota comes from the session rollouts.
    public static var liveSourceIfConfigured: (any QuotaSource)? {
        ProcessInfo.processInfo.environment["QUOTA_MONITOR_CODEX_USAGE_URL"]
            .flatMap(URL.init(string:))
            .map { CodexLiveSource(endpoint: $0) }
    }

    /// Builds a `QuotaWindow` from Codex's `{used_percent, window_minutes, resets_at}`.
    static func window(from value: JSONValue, id: String) -> QuotaWindow? {
        guard let used = value.firstValue(forAnyKey: ["used_percent", "usedPercent"])?.double else {
            return nil
        }
        let minutes = value.firstValue(forAnyKey: ["window_minutes", "windowDurationMins", "windowMinutes"])?.int
        return QuotaWindow(
            id: id,
            label: QuotaWindow.label(forMinutes: minutes),
            kind: .inferred(fromMinutes: minutes),
            usedPercent: used,
            resetsAt: value.firstValue(forAnyKey: ["resets_at", "resetsAt"])?.date,
            windowMinutes: minutes
        )
    }

    /// Parses a `rate_limits` object into a snapshot.
    static func snapshot(
        fromRateLimits limits: JSONValue,
        observedAt: Date,
        origin: SnapshotOrigin
    ) -> ProviderSnapshot? {
        var windows: [QuotaWindow] = []
        if let primary = limits["primary"], let w = window(from: primary, id: "primary") {
            windows.append(w)
        }
        if let secondary = limits["secondary"], let w = window(from: secondary, id: "secondary") {
            windows.append(w)
        }
        guard !windows.isEmpty else { return nil }

        var credits: Credits?
        if let c = limits["credits"] {
            let hasCredits = c.firstValue(forAnyKey: ["has_credits", "hasCredits"])?.bool ?? false
            let unlimited = c["unlimited"]?.bool ?? false
            credits = Credits(
                hasCredits: hasCredits,
                unlimited: unlimited,
                balance: c["balance"]?.string,
                enabled: hasCredits || unlimited
            )
        }

        return ProviderSnapshot(
            id: providerID,
            displayName: displayName,
            plan: limits.firstValue(forAnyKey: ["plan_type", "planType"])?.string,
            windows: windows,
            credits: credits,
            observedAt: observedAt,
            origin: origin
        )
    }
}

// MARK: - Local

/// Reads the freshest `rate_limits` record Codex left in its session rollouts.
///
/// Codex writes a `token_count` event after every turn carrying the full rate
/// limit payload, so the newest rollout file is an accurate record of where you
/// stood at the end of your last Codex turn.
public struct CodexLocalSource: QuotaSource {
    public let providerID = Codex.providerID
    public let displayName = Codex.displayName
    public let origin = SnapshotOrigin.local

    private let sessionsRoot: URL
    /// Only the newest few files are worth opening.
    private let maxFilesToScan: Int
    /// The payload sits near the end of a rollout, so read the tail rather than
    /// the whole file — these can reach many megabytes.
    private let tailBytes: Int

    public init(
        home: URL = Codex.defaultHome,
        maxFilesToScan: Int = 16,
        tailBytes: Int = 512 * 1024
    ) {
        self.sessionsRoot = home.appendingPathComponent("sessions")
        self.maxFilesToScan = maxFilesToScan
        self.tailBytes = tailBytes
    }

    public func fetch() async throws -> ProviderSnapshot {
        guard FileManager.default.fileExists(atPath: sessionsRoot.path) else {
            throw QuotaError.notConfigured("No Codex sessions found at \(sessionsRoot.path)")
        }

        for file in try newestRollouts() {
            guard let line = try Self.lastLine(containing: "rate_limits", in: file, tailBytes: tailBytes),
                  let root = try? JSONValue.parse(Data(line.utf8)),
                  let limits = root.firstValue(forKey: "rate_limits"),
                  let observedAt = root["timestamp"]?.date
                    ?? file.contentModifiedDate,
                  var snapshot = Codex.snapshot(
                    fromRateLimits: limits,
                    observedAt: observedAt,
                    origin: .local
                  )
            else { continue }

            let now = Date()
            if !snapshot.windows.contains(where: { $0.currentUsedPercent(asOf: now) != nil }) {
                snapshot.status = .needsSetup(
                    "ChatGPT reports usage only after a Codex turn — last reading \(QuotaFormat.age(snapshot.age(asOf: now)))"
                )
            }
            return snapshot
        }

        throw QuotaError.noDataFound("No rate limit records in the last \(maxFilesToScan) Codex sessions")
    }

    private func newestRollouts() throws -> [URL] {
        let keys: [URLResourceKey] = [.contentModificationDateKey, .isRegularFileKey]
        guard let walker = FileManager.default.enumerator(
            at: sessionsRoot,
            includingPropertiesForKeys: keys,
            options: [.skipsHiddenFiles]
        ) else { return [] }

        var candidates: [(url: URL, date: Date)] = []
        for case let url as URL in walker where url.pathExtension == "jsonl" {
            guard let date = url.contentModifiedDate else { continue }
            candidates.append((url, date))
        }
        return candidates
            .sorted { $0.date > $1.date }
            .prefix(maxFilesToScan)
            .map(\.url)
    }

    /// Last whole line in the file containing `needle`.
    ///
    /// Reads only the tail first, since the record we want is near the end. If
    /// that window lands mid-record it would yield an unparseable fragment, so
    /// the partial leading line is dropped — and if that leaves no match, the
    /// search escalates to the whole file rather than reporting nothing.
    static func lastLine(containing needle: String, in url: URL, tailBytes: Int) throws -> String? {
        if let hit = try lastLine(containing: needle, in: url, readingLastBytes: tailBytes) {
            return hit
        }
        let size = try fileSize(of: url)
        guard size > UInt64(tailBytes) else { return nil }
        return try lastLine(containing: needle, in: url, readingLastBytes: nil)
    }

    /// - Parameter readingLastBytes: nil reads the entire file.
    private static func lastLine(
        containing needle: String,
        in url: URL,
        readingLastBytes limit: Int?
    ) throws -> String? {
        let handle = try FileHandle(forReadingFrom: url)
        defer { try? handle.close() }

        let size = try handle.seekToEnd()
        let truncated = limit.map { size > UInt64($0) } ?? false
        try handle.seek(toOffset: truncated ? size - UInt64(limit!) : 0)
        guard let data = try handle.readToEnd(), !data.isEmpty else { return nil }

        var text = String(decoding: data, as: UTF8.self)
        if truncated, let firstBreak = text.firstIndex(of: "\n") {
            text = String(text[text.index(after: firstBreak)...])
        }

        return text
            .split(separator: "\n", omittingEmptySubsequences: true)
            .last { $0.contains(needle) }
            .map(String.init)
    }

    private static func fileSize(of url: URL) throws -> UInt64 {
        let handle = try FileHandle(forReadingFrom: url)
        defer { try? handle.close() }
        return try handle.seekToEnd()
    }
}

// MARK: - Live

/// Queries ChatGPT's backend for current Codex usage.
///
/// Best-effort: this endpoint is private and its exact path/response are not
/// documented, so any failure falls back to `CodexLocalSource`.
public struct CodexLiveSource: QuotaSource {
    public let providerID = Codex.providerID
    public let displayName = Codex.displayName
    public let origin = SnapshotOrigin.live

    private let authFile: URL
    private let endpoint: URL
    private let session: URLSession

    /// Overridable without a rebuild — this path is undocumented and may move.
    public static var defaultEndpoint: URL {
        ProcessInfo.processInfo.environment["QUOTA_MONITOR_CODEX_USAGE_URL"]
            .flatMap(URL.init(string:))
            ?? URL(string: "https://chatgpt.com/backend-api/api/codex/usage")!
    }

    public init(
        home: URL = Codex.defaultHome,
        endpoint: URL = CodexLiveSource.defaultEndpoint,
        session: URLSession = .shared
    ) {
        self.authFile = home.appendingPathComponent("auth.json")
        self.endpoint = endpoint
        self.session = session
    }

    public func fetch() async throws -> ProviderSnapshot {
        // Scoped to `tokens` rather than searched recursively, so a future key
        // elsewhere in the file can never be mistaken for the access token.
        guard let data = try? Data(contentsOf: authFile),
              let auth = try? JSONValue.parse(data),
              let token = (auth["tokens"] ?? auth)["access_token"]?.string,
              !token.isEmpty
        else {
            throw QuotaError.notConfigured("Sign in with `codex login` — no ChatGPT token in \(authFile.path)")
        }

        var request = URLRequest(url: endpoint)
        request.timeoutInterval = 15
        request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        request.setValue("application/json", forHTTPHeaderField: "Accept")
        // The backend routes on these; without them it answers 404 rather than 401.
        request.setValue("codex_cli_rs", forHTTPHeaderField: "originator")
        request.setValue("codex_cli_rs/0.146.0", forHTTPHeaderField: "User-Agent")
        if let accountID = (auth["tokens"] ?? auth)["account_id"]?.string {
            request.setValue(accountID, forHTTPHeaderField: "ChatGPT-Account-Id")
        }

        let (body, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse else {
            throw QuotaError.transport("No HTTP response from \(endpoint.host() ?? "endpoint")")
        }
        guard (200..<300).contains(http.statusCode) else {
            throw QuotaError.forHTTP(http.statusCode, provider: "ChatGPT")
        }

        let root = try JSONValue.parse(body)
        let limits = root.firstValue(forKey: "rate_limits") ?? root
        guard let snapshot = Codex.snapshot(fromRateLimits: limits, observedAt: Date(), origin: .live) else {
            throw QuotaError.malformed("Unrecognised response from ChatGPT usage endpoint")
        }
        return snapshot
    }
}

extension URL {
    var contentModifiedDate: Date? {
        try? resourceValues(forKeys: [.contentModificationDateKey]).contentModificationDate
    }
}
