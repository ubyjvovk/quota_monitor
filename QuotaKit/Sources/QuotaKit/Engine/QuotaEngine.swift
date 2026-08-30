import Foundation
import Observation

/// Owns the provider set, refreshes them, and publishes the result.
///
/// Adding a provider means adding one `HybridProvider` to `ProviderCatalog.all`
/// — the UI is driven entirely off the normalised snapshot and needs no changes.
@MainActor
@Observable
public final class QuotaEngine {
    public private(set) var snapshot: QuotaSnapshot
    public private(set) var history: UsageHistory
    public private(set) var isRefreshing = false
    public private(set) var lastRefreshedAt: Date?
    /// The most recent runner refresh error, cleared after a successful refresh.
    public private(set) var lastError: String?

    public var settings: QuotaSettings {
        didSet {
            guard settings != oldValue else { return }
            settings.save()
            restartAutoRefresh()
        }
    }

    private let store: SnapshotStore
    private let historyStore: HistoryStore
    private let runner: QuotamonRunner?
    private var refreshTask: Task<Void, Never>?

    public var isSharedWithWidget: Bool { store.isSharedWithWidget }

    /// Creates an engine, optionally sourcing snapshots from the Go core runner.
    public init(
        store: SnapshotStore = SnapshotStore(),
        settings: QuotaSettings = .load(),
        runner: QuotamonRunner? = nil
    ) {
        self.store = store
        self.historyStore = HistoryStore(store: store)
        self.runner = runner
        self.settings = settings
        // Show the last known numbers immediately rather than an empty window.
        self.snapshot = store.load() ?? .empty
        self.history = historyStore.load()
    }

    /// Fixed data, no disk access — for SwiftUI previews and design rendering.
    public init(preview snapshot: QuotaSnapshot, history: UsageHistory = .preview) {
        let scratch = SnapshotStore(appGroupID: nil)
        self.store = scratch
        self.historyStore = HistoryStore(store: scratch)
        self.runner = nil
        self.settings = QuotaSettings()
        self.snapshot = snapshot
        self.history = history
        self.lastRefreshedAt = snapshot.generatedAt
    }

    // MARK: - Providers

    private func makeProviders() -> [HybridProvider] {
        ProviderCatalog.all(isLiveEnabled: settings.isLiveEnabled)
    }

    // MARK: - Refresh

    /// Refreshes quota data using normal cache behavior.
    public func refresh() async {
        await refresh(fresh: false)
    }

    /// Refreshes quota data, asking the Go core to bypass its caches when requested.
    public func refresh(fresh: Bool) async {
        guard !isRefreshing else { return }
        isRefreshing = true
        defer { isRefreshing = false }

        if let runner {
            do {
                let freshSnapshot = try await runner.snapshot(fresh: fresh)
                accept(freshSnapshot)
                lastError = nil
            } catch {
                lastError = error.localizedDescription
            }
            return
        }

        let providers = makeProviders()
        var results: [ProviderSnapshot] = await withTaskGroup(of: ProviderSnapshot.self) { group in
            for provider in providers {
                group.addTask { await provider.fetch() }
            }
            var collected: [ProviderSnapshot] = []
            for await result in group { collected.append(result) }
            return collected
        }

        // Task groups complete out of order; keep the declared provider order stable.
        let order = providers.map(\.providerID)
        results.sort {
            (order.firstIndex(of: $0.id) ?? .max) < (order.firstIndex(of: $1.id) ?? .max)
        }

        let freshSnapshot = QuotaSnapshot(providers: results, generatedAt: Date())
        accept(freshSnapshot)
        lastError = nil
    }

    private func accept(_ freshSnapshot: QuotaSnapshot) {
        let now = Date()
        snapshot = freshSnapshot
        lastRefreshedAt = now

        var updated = history
        updated.record(freshSnapshot, at: now)
        history = updated

        try? store.save(freshSnapshot)
        try? historyStore.save(updated)
        WidgetRefresher.reloadAll()
    }

    /// Trajectory for one window, for sparklines.
    public func samples(provider: String, window: String) -> [UsageSample] {
        history.samples(provider: provider, window: window)
    }

    // MARK: - Auto refresh

    public func startAutoRefresh() {
        restartAutoRefresh()
    }

    private func restartAutoRefresh() {
        refreshTask?.cancel()
        let interval = max(30, settings.refreshInterval)
        refreshTask = Task { [weak self] in
            while !Task.isCancelled {
                // Exits on the next tick once the engine is gone, so the loop
                // cannot outlive its owner.
                guard let self else { return }
                await self.refresh()
                try? await Task.sleep(for: .seconds(interval))
            }
        }
    }

    public func stopAutoRefresh() {
        refreshTask?.cancel()
        refreshTask = nil
    }
}

extension SnapshotStore {
    // Tests need a real file-backed store without reaching into Application Support.
    init(testDirectory directory: URL) {
        self.directory = directory
        self.isSharedWithWidget = false
    }
}
