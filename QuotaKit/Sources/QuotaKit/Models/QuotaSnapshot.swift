import Foundation

/// The whole picture across every configured provider. This is the unit that is
/// persisted and handed to the widget.
public struct QuotaSnapshot: Codable, Hashable, Sendable {
    public var providers: [ProviderSnapshot]
    public var generatedAt: Date

    public init(providers: [ProviderSnapshot], generatedAt: Date = Date()) {
        self.providers = providers
        self.generatedAt = generatedAt
    }

    public static let empty = QuotaSnapshot(providers: [], generatedAt: .distantPast)

    /// Providers that produced a usable reading, most-constrained first.
    public func rankedProviders(asOf now: Date = Date()) -> [ProviderSnapshot] {
        providers.sorted {
            let a = $0.tightestWindow(asOf: now)?.effectiveUsedPercent(asOf: now) ?? -1
            let b = $1.tightestWindow(asOf: now)?.effectiveUsedPercent(asOf: now) ?? -1
            if a != b { return a > b }
            return $0.displayName < $1.displayName
        }
    }

    /// The single most-constrained (provider, window) pair anywhere — the number
    /// that belongs in the menu bar.
    public func headline(asOf now: Date = Date()) -> (provider: ProviderSnapshot, window: QuotaWindow)? {
        var best: (ProviderSnapshot, QuotaWindow)?
        for provider in providers {
            guard let window = provider.tightestWindow(asOf: now) else { continue }
            if let current = best,
               current.1.effectiveUsedPercent(asOf: now) >= window.effectiveUsedPercent(asOf: now) {
                continue
            }
            best = (provider, window)
        }
        return best
    }

    // MARK: - Codable across versions

    /// Decoding is deliberately forgiving: a snapshot written by an older build
    /// should never crash a newer widget, it should just render what it can.
    public static func decode(from data: Data) throws -> QuotaSnapshot {
        try JSONDecoder.quota.decode(QuotaSnapshot.self, from: data)
    }

    public func encoded() throws -> Data {
        try JSONEncoder.quota.encode(self)
    }
}

extension JSONDecoder {
    static let quota: JSONDecoder = {
        let d = JSONDecoder()
        d.dateDecodingStrategy = .iso8601
        return d
    }()
}

extension JSONEncoder {
    static let quota: JSONEncoder = {
        let e = JSONEncoder()
        e.dateEncodingStrategy = .iso8601
        e.outputFormatting = [.prettyPrinted, .sortedKeys]
        return e
    }()
}
