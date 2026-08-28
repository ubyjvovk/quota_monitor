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
            if case .failure(let error)? = liveOutcome {
                snapshot.status = .needsSetup("Cached — live refresh failed: \(error.localizedDescription)")
            }
            return snapshot
        }

        // Nothing usable. The local failure is normally the one worth acting on.
        let message = localOutcome?.failureMessage
            ?? liveOutcome?.failureMessage
            ?? "No data source configured"
        return .unavailable(id: providerID, displayName: displayName, status: .needsSetup(message))
    }

    private static func attempt(_ source: (any QuotaSource)?) async -> Result<ProviderSnapshot, any Error>? {
        guard let source else { return nil }
        do { return .success(try await source.fetch()) }
        catch { return .failure(error) }
    }
}

private extension Result where Success == ProviderSnapshot, Failure == any Error {
    var failureMessage: String? {
        if case .failure(let error) = self { return error.localizedDescription }
        return nil
    }
}
