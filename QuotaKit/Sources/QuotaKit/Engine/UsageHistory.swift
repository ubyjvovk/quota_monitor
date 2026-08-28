import Foundation

/// Usage readings over time, one series per (provider, window).
///
/// Kept so the UI can show a trajectory rather than a single number: a bare
/// "43%" cannot distinguish a steady burn from a spike ten minutes ago.
public struct UsageHistory: Codable, Hashable, Sendable {
    /// Keyed by "provider.window".
    public var series: [String: [UsageSample]]

    /// Enough points for a smooth sparkline, few enough to stay small on disk.
    static let maxSamplesPerSeries = 240

    public init(series: [String: [UsageSample]] = [:]) {
        self.series = series
    }

    public static func key(provider: String, window: String) -> String {
        "\(provider).\(window)"
    }

    public func samples(provider: String, window: String) -> [UsageSample] {
        series[Self.key(provider: provider, window: window)] ?? []
    }

    /// Adds one reading per window, dropping anything from a previous window.
    ///
    /// Pruning at the window boundary is what keeps a sparkline meaningful —
    /// carrying last week's climb into this week's chart would draw a cliff at
    /// the reset that says nothing about current consumption.
    public mutating func record(_ snapshot: QuotaSnapshot, at now: Date = Date()) {
        for provider in snapshot.providers {
            for window in provider.windows {
                let key = Self.key(provider: provider.id, window: window.id)
                var samples = series[key] ?? []

                if let start = window.windowStart() {
                    samples.removeAll { $0.at < start }
                }

                let used = window.effectiveUsedPercent(asOf: now)
                // Skip duplicate readings; local sources repeat between CLI turns
                // and a flat run of identical points is noise, not signal.
                if let last = samples.last, last.usedPercent == used,
                   now.timeIntervalSince(last.at) < 600 {
                    continue
                }

                samples.append(UsageSample(at: now, usedPercent: used))
                if samples.count > Self.maxSamplesPerSeries {
                    samples.removeFirst(samples.count - Self.maxSamplesPerSeries)
                }
                series[key] = samples
            }
        }
    }
}

/// Persists `UsageHistory` beside the snapshot.
public struct HistoryStore: Sendable {
    public static let fileName = "history.json"

    private let directory: URL

    public init(directory: URL) {
        self.directory = directory
    }

    public init(store: SnapshotStore = SnapshotStore()) {
        self.directory = store.directory
    }

    public var fileURL: URL { directory.appendingPathComponent(Self.fileName) }

    public func load() -> UsageHistory {
        guard let data = try? Data(contentsOf: fileURL),
              let decoded = try? JSONDecoder.quota.decode(UsageHistory.self, from: data)
        else { return UsageHistory() }
        return decoded
    }

    public func save(_ history: UsageHistory) throws {
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        try JSONEncoder.quota.encode(history).write(to: fileURL, options: .atomic)
    }
}
