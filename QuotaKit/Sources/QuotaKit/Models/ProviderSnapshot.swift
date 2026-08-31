import Foundation

/// Where a reading came from. Surfaced in the UI so a stale local reading is
/// never mistaken for a live one.
public enum SnapshotOrigin: String, Codable, Sendable {
    /// Fetched from the provider's usage endpoint just now.
    case live
    /// Read from an on-disk artefact the CLI left behind. Only as fresh as the
    /// last time you actually used that CLI.
    case local
    /// Nothing readable was found.
    case unavailable

    public var displayName: String {
        switch self {
        case .live: "live"
        case .local: "cached"
        case .unavailable: "unavailable"
        }
    }
}

public enum ProviderStatus: Codable, Hashable, Sendable {
    case ok
    /// Readable, but the user must do something (install the mirror, log in).
    case needsSetup(String)
    /// Tried and failed.
    case failed(String)

    public var message: String? {
        switch self {
        case .ok: nil
        case .needsSetup(let m), .failed(let m): m
        }
    }

    public var isOK: Bool { if case .ok = self { true } else { false } }

    private enum CodingKeys: String, CodingKey {
        case state, message
        case ok, needsSetup, failed
    }

    private enum LegacyPayloadKeys: String, CodingKey {
        case value = "_0"
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)

        if container.contains(.state) {
            switch try container.decode(String.self, forKey: .state) {
            case "ok":
                self = .ok
            case "needsSetup":
                self = .needsSetup(try container.decode(String.self, forKey: .message))
            case "failed":
                self = .failed(try container.decode(String.self, forKey: .message))
            case let state:
                throw DecodingError.dataCorruptedError(
                    forKey: .state,
                    in: container,
                    debugDescription: "Unknown provider status state \(state)"
                )
            }
            return
        }

        if container.contains(.ok) {
            _ = try container.nestedContainer(keyedBy: LegacyPayloadKeys.self, forKey: .ok)
            self = .ok
            return
        }

        for (key, makeStatus) in [
            (CodingKeys.needsSetup, ProviderStatus.needsSetup),
            (CodingKeys.failed, ProviderStatus.failed),
        ] where container.contains(key) {
            let payload = try container.nestedContainer(
                keyedBy: LegacyPayloadKeys.self,
                forKey: key
            )
            self = makeStatus(try payload.decode(String.self, forKey: .value))
            return
        }

        throw DecodingError.dataCorrupted(
            .init(codingPath: decoder.codingPath, debugDescription: "Unknown provider status shape")
        )
    }

    public func encode(to encoder: Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        switch self {
        case .ok:
            try container.encode("ok", forKey: .state)
        case .needsSetup(let message):
            try container.encode("needsSetup", forKey: .state)
            try container.encode(message, forKey: .message)
        case .failed(let message):
            try container.encode("failed", forKey: .state)
            try container.encode(message, forKey: .message)
        }
    }
}

public struct Credits: Codable, Hashable, Sendable {
    public var hasCredits: Bool
    public var unlimited: Bool
    public var balance: String?
    /// Provider-reported spend when it is distinct from the remaining balance.
    public var spend: String?
    /// Whether the balance can actually be spent. A provider can report a
    /// non-zero balance that is switched off (Claude: `spend.enabled == false`
    /// with `disabled_reason`), and showing that as available headroom is the
    /// one thing this tool must never do.
    public var enabled: Bool

    /// Creates a provider-reported credit balance.
    public init(
        hasCredits: Bool,
        unlimited: Bool,
        balance: String? = nil,
        enabled: Bool = true,
        spend: String? = nil
    ) {
        self.hasCredits = hasCredits
        self.unlimited = unlimited
        self.balance = balance
        self.enabled = enabled
        self.spend = spend
    }

    private enum CodingKeys: String, CodingKey {
        case hasCredits, unlimited, balance, enabled, spend
    }

    /// Decodes older persisted balances as enabled when they predate the flag.
    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        self.hasCredits = try container.decode(Bool.self, forKey: .hasCredits)
        self.unlimited = try container.decode(Bool.self, forKey: .unlimited)
        self.balance = try container.decodeIfPresent(String.self, forKey: .balance)
        self.enabled = try container.decodeIfPresent(Bool.self, forKey: .enabled) ?? true
        self.spend = try container.decodeIfPresent(String.self, forKey: .spend)
    }
}

/// One provider's quota picture at a point in time.
public struct ProviderSnapshot: Codable, Hashable, Sendable, Identifiable {
    public var id: String
    public var displayName: String
    /// Subscription tier as the provider reports it, e.g. `"plus"`, `"max"`.
    public var plan: String?
    public var windows: [QuotaWindow]
    public var credits: Credits?
    /// When these numbers were actually true — *not* when we read them.
    public var observedAt: Date
    public var origin: SnapshotOrigin
    public var status: ProviderStatus

    public init(
        id: String,
        displayName: String,
        plan: String? = nil,
        windows: [QuotaWindow] = [],
        credits: Credits? = nil,
        observedAt: Date,
        origin: SnapshotOrigin,
        status: ProviderStatus = .ok
    ) {
        self.id = id
        self.displayName = displayName
        self.plan = plan
        self.windows = windows
        self.credits = credits
        self.observedAt = observedAt
        self.origin = origin
        self.status = status
    }

    /// A provider with nothing readable — rendered as a dash rather than a zero,
    /// so "no data" never looks like "no usage".
    public static func unavailable(
        id: String,
        displayName: String,
        status: ProviderStatus,
        observedAt: Date = Date()
    ) -> ProviderSnapshot {
        ProviderSnapshot(
            id: id,
            displayName: displayName,
            observedAt: observedAt,
            origin: .unavailable,
            status: status
        )
    }

    /// Windows sorted for display: most-constrained first, unreadable ones last.
    public func sortedWindows(asOf now: Date = Date()) -> [QuotaWindow] {
        windows.sorted {
            switch ($0.currentUsedPercent(asOf: now), $1.currentUsedPercent(asOf: now)) {
            case let (a?, b?):
                if a != b { return a > b }
                return $0.kind.displayRank < $1.kind.displayRank
            case (_?, nil): return true    // a real reading outranks a missing one
            case (nil, _?): return false
            case (nil, nil): return $0.kind.displayRank < $1.kind.displayRank
            }
        }
    }

    /// The window closest to its limit — what the menu bar shows when space is
    /// tight. Only windows we actually have a reading for can qualify.
    public func tightestWindow(asOf now: Date = Date()) -> QuotaWindow? {
        sortedWindows(asOf: now).first { $0.currentUsedPercent(asOf: now) != nil }
    }

    /// How long ago these numbers were true.
    public func age(asOf now: Date = Date()) -> TimeInterval {
        max(0, now.timeIntervalSince(observedAt))
    }

    /// Short tag for the menu bar, where width is scarce.
    ///
    /// Deliberately not the first letter: "Claude" and "ChatGPT" would collide
    /// and render as "C 43% · C 18%". Computed rather than stored so older
    /// persisted snapshots still decode.
    public var shortName: String {
        Self.shortNames[id] ?? String(displayName.prefix(2)).uppercased()
    }

    static let shortNames: [String: String] = [
        // Keep in sync with core/cmd/quotamon/waybar.go, the compact-name catalog.
        Claude.providerID: "CL",
        Codex.providerID: "GPT",
        "grok": "GK",
        "deepinfra": "DI",
        "kimi": "KM",
        "runinfra": "RI",
    ]
}
