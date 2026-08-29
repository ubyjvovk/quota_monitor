import Foundation

public enum QuotaError: LocalizedError, Sendable {
    case notConfigured(String)
    case noDataFound(String)
    case unauthorized(String)
    case transport(String)
    case malformed(String)

    public var errorDescription: String? {
        switch self {
        case .notConfigured(let m): m
        case .noDataFound(let m): m
        case .unauthorized(let m): m
        case .transport(let m): m
        case .malformed(let m): m
        }
    }

    /// Whether re-running immediately could plausibly succeed.
    public var isTransient: Bool {
        if case .transport = self { return true }
        return false
    }

    /// How strongly this failure should claim the one line of status the UI has.
    ///
    /// A configured source that broke outranks an optional one that was never
    /// set up: reporting "statusline not installed" while the real problem is an
    /// expired token sends the user to fix the wrong thing.
    var reportingPriority: Int {
        switch self {
        case .unauthorized: 4   // actionable, and the actual blocker
        case .malformed: 3
        case .transport: 2
        case .noDataFound: 1
        case .notConfigured: 0  // an optional fallback nobody asked for
        }
    }
}

public extension QuotaError {
    /// Maps an HTTP status onto the right kind of failure.
    ///
    /// The distinction matters: a 429 is temporary and the cached reading stays
    /// trustworthy, whereas a 401 means the user has to re-authenticate.
    static func forHTTP(_ status: Int, provider: String) -> QuotaError {
        switch status {
        case 401, 403:
            .unauthorized("\(provider) rejected the token — sign in again")
        case 429:
            .transport("\(provider) is rate limiting usage checks — will retry")
        case 500...599:
            .transport("\(provider) usage endpoint is unavailable (HTTP \(status))")
        default:
            .transport("\(provider) usage endpoint returned HTTP \(status)")
        }
    }
}

/// One way of obtaining a provider's quota — either a local artefact or a live
/// endpoint. A provider normally has one of each.
public protocol QuotaSource: Sendable {
    var providerID: String { get }
    var displayName: String { get }
    var origin: SnapshotOrigin { get }
    func fetch() async throws -> ProviderSnapshot
}

/// Pairs a local and a live source for one provider and decides which reading wins.
public struct HybridProvider: Sendable {
    public let providerID: String
    public let displayName: String
    public let local: (any QuotaSource)?
    public let live: (any QuotaSource)?
    /// When false, the live source is skipped entirely — no network, no tokens read.
    public var liveEnabled: Bool

    public init(
        providerID: String,
        displayName: String,
        local: (any QuotaSource)?,
        live: (any QuotaSource)?,
        liveEnabled: Bool = true
    ) {
        self.providerID = providerID
        self.displayName = displayName
        self.local = local
        self.live = live
        self.liveEnabled = liveEnabled
    }

    /// Live wins when it succeeds, because it is current by definition. Otherwise
    /// fall back to the local reading and keep the live failure as the status, so
    /// the UI can show the cached number *and* say why it isn't live.
    ///
    /// Failures are carried rather than swallowed: "install the statusline mirror"
    /// is actionable, "no data" is not.
    public func fetch() async -> ProviderSnapshot {
        async let liveAttempt = Self.attempt(liveEnabled ? live : nil)
        async let localAttempt = Self.attempt(local)
        let (liveOutcome, localOutcome) = await (liveAttempt, localAttempt)

        if case .success(let snapshot)? = liveOutcome, !snapshot.windows.isEmpty {
            return snapshot
        }

        if case .success(var snapshot)? = localOutcome, !snapshot.windows.isEmpty {
            let now = Date()
            let hasCurrentReading = snapshot.windows.contains {
                $0.currentUsedPercent(asOf: now) != nil
            }

            if !hasCurrentReading {
                // Every window reset since this was recorded, so the numbers
                // describe windows that no longer exist. That is the headline —
                // a live-refresh error underneath it is noise by comparison.
                snapshot.status = .needsSetup(
                    "Last reading \(QuotaFormat.age(snapshot.age(asOf: now))); its window has since reset"
                )
            } else if case .failure(let error)? = liveOutcome {
                snapshot.status = .needsSetup("Cached — live refresh failed: \(error.localizedDescription)")
            }
            return snapshot
        }

        // Nothing usable. Report whichever failure the user can actually act on.
        let message = Self.mostActionableMessage(localOutcome, liveOutcome)
            ?? "No data source configured"
        return .unavailable(id: providerID, displayName: displayName, status: .needsSetup(message))
    }

    /// Highest-priority failure across the attempts, local winning ties.
    private static func mostActionableMessage(
        _ outcomes: Result<ProviderSnapshot, any Error>?...
    ) -> String? {
        outcomes
            .compactMap { outcome -> (Int, String)? in
                guard case .failure(let error)? = outcome else { return nil }
                let priority = (error as? QuotaError)?.reportingPriority ?? 2
                return (priority, error.localizedDescription)
            }
            .max { $0.0 < $1.0 }?
            .1
    }

    private static func attempt(_ source: (any QuotaSource)?) async -> Result<ProviderSnapshot, any Error>? {
        guard let source else { return nil }
        do { return .success(try await source.fetch()) }
        catch { return .failure(error) }
    }
}
