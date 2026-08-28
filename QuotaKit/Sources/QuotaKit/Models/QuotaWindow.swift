import Foundation

/// A single rate-limit window for a provider, normalised across vendors.
///
/// Claude reports `five_hour` / `seven_day` as `used_percentage`; Codex reports
/// `primary` / `secondary` as `used_percent` with a `window_minutes` duration.
/// Both collapse into this shape.
public struct QuotaWindow: Codable, Hashable, Sendable, Identifiable {

    public enum Kind: String, Codable, Sendable {
        case session   // short rolling window (Claude's 5-hour)
        case weekly    // 7-day window
        case monthly
        case other

        /// Sort order for display: tightest/most-urgent window kinds first.
        var displayRank: Int {
            switch self {
            case .session: 0
            case .weekly: 1
            case .monthly: 2
            case .other: 3
            }
        }
    }

    /// Stable within a provider, e.g. `"five_hour"`, `"primary"`.
    public var id: String
    /// Short label for compact UI, e.g. `"5h"`, `"Week"`.
    public var label: String
    public var kind: Kind
    /// 0...100. May exceed 100 if a provider reports overage.
    public var usedPercent: Double
    /// When this window rolls over and usage returns to zero.
    public var resetsAt: Date?
    /// Total window length, when the provider reports one.
    public var windowMinutes: Int?

    public init(
        id: String,
        label: String,
        kind: Kind,
        usedPercent: Double,
        resetsAt: Date? = nil,
        windowMinutes: Int? = nil
    ) {
        self.id = id
        self.label = label
        self.kind = kind
        self.usedPercent = usedPercent
        self.resetsAt = resetsAt
        self.windowMinutes = windowMinutes
    }

    /// True once `resetsAt` has passed — any usage recorded before that point no
    /// longer applies.
    ///
    /// This matters because local sources are snapshots of the last CLI turn: a
    /// reading of "82% used" taken last Tuesday is meaningless if the weekly
    /// window rolled over on Thursday.
    public func hasRolledOver(asOf now: Date = Date()) -> Bool {
        guard let resetsAt else { return false }
        return resetsAt <= now
    }

    /// Current usage, or nil when the window reset after this reading was taken.
    ///
    /// A rolled-over window means the number we hold describes a window that no
    /// longer exists. That is *unknown*, not zero — reporting 0% would assert a
    /// fresh, empty window we have no evidence for. UI should render nil as a
    /// dash; only ordering may treat it as a low value.
    public func currentUsedPercent(asOf now: Date = Date()) -> Double? {
        hasRolledOver(asOf: now) ? nil : usedPercent
    }

    /// Usage corrected for rollover. Prefer this over `usedPercent` for display.
    public func effectiveUsedPercent(asOf now: Date = Date()) -> Double {
        hasRolledOver(asOf: now) ? 0 : usedPercent
    }

    public func effectiveRemainingPercent(asOf now: Date = Date()) -> Double {
        max(0, 100 - effectiveUsedPercent(asOf: now))
    }

    /// Time until this window resets, or nil if unknown/already passed.
    public func timeUntilReset(asOf now: Date = Date()) -> TimeInterval? {
        guard let resetsAt, resetsAt > now else { return nil }
        return resetsAt.timeIntervalSince(now)
    }
}

public extension QuotaWindow.Kind {
    /// Infers a window kind from its duration. 10080 minutes == 7 days.
    static func inferred(fromMinutes minutes: Int?) -> QuotaWindow.Kind {
        guard let minutes else { return .other }
        if minutes < 60 * 24 { return .session }
        if minutes < 60 * 24 * 10 { return .weekly }
        return .monthly
    }
}

public extension QuotaWindow {
    /// Human label derived from a window duration, e.g. 300 -> "5h", 10080 -> "Week".
    static func label(forMinutes minutes: Int?) -> String {
        guard let minutes else { return "Usage" }
        if minutes % (60 * 24) == 0 {
            let days = minutes / (60 * 24)
            return days == 7 ? "Week" : "\(days)d"
        }
        if minutes % 60 == 0 { return "\(minutes / 60)h" }
        return "\(minutes)m"
    }
}
