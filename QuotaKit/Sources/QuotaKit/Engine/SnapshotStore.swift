import Foundation

/// The handoff point between the app (which fetches) and the widget (which only reads).
///
/// Prefers the App Group container so a sandboxed widget extension can read it.
/// Falls back to Application Support when no group container exists — which is
/// what happens without a signing team. In that state the menu bar app still works
/// completely; only the widget goes without data.
public struct SnapshotStore: Sendable {
    public static let defaultAppGroupID = "group.dev.quotamonitor.shared"
    public static let fileName = "snapshot.json"

    public let directory: URL
    /// True when we resolved a real App Group container, i.e. the widget can see this.
    public let isSharedWithWidget: Bool

    public init(appGroupID: String? = SnapshotStore.defaultAppGroupID) {
        if let appGroupID,
           let container = FileManager.default
            .containerURL(forSecurityApplicationGroupIdentifier: appGroupID) {
            self.directory = container.appendingPathComponent("QuotaMonitor", isDirectory: true)
            self.isSharedWithWidget = true
        } else {
            let support = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask)[0]
            self.directory = support.appendingPathComponent("QuotaMonitor", isDirectory: true)
            self.isSharedWithWidget = false
        }
    }

    public var fileURL: URL { directory.appendingPathComponent(Self.fileName) }

    public func save(_ snapshot: QuotaSnapshot) throws {
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        // Atomic so a widget reading mid-write never sees a truncated file.
        try snapshot.encoded().write(to: fileURL, options: .atomic)
    }

    public func load() -> QuotaSnapshot? {
        guard let data = try? Data(contentsOf: fileURL) else { return nil }
        return try? QuotaSnapshot.decode(from: data)
    }
}

/// User-tunable behaviour, persisted so the app and widget agree.
public struct QuotaSettings: Codable, Hashable, Sendable {
    public var refreshInterval: TimeInterval
    /// Per-provider switch for hitting the live endpoint. Off means local files only.
    public var liveEnabled: [String: Bool]

    public init(
        refreshInterval: TimeInterval = 300,
        liveEnabled: [String: Bool] = [Claude.providerID: true, Codex.providerID: true]
    ) {
        self.refreshInterval = refreshInterval
        self.liveEnabled = liveEnabled
    }

    public func isLiveEnabled(_ providerID: String) -> Bool {
        liveEnabled[providerID] ?? true
    }

    private static let defaultsKey = "QuotaSettings"

    public static func load(from defaults: UserDefaults = .standard) -> QuotaSettings {
        guard let data = defaults.data(forKey: defaultsKey),
              let decoded = try? JSONDecoder().decode(QuotaSettings.self, from: data)
        else { return QuotaSettings() }
        return decoded
    }

    public func save(to defaults: UserDefaults = .standard) {
        guard let data = try? JSONEncoder().encode(self) else { return }
        defaults.set(data, forKey: Self.defaultsKey)
    }
}
