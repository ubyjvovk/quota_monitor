import Foundation

/// A single observation of one window's usage.
public struct UsageSample: Codable, Hashable, Sendable {
    public var at: Date
    public var usedPercent: Double

    public init(at: Date, usedPercent: Double) {
        self.at = at
        self.usedPercent = usedPercent
    }
}

/// Usage measured against the clock.
///
/// "43% used" is not actionable on its own — 43% is fine four hours into a
/// five-hour window and alarming ten minutes in. What matters is whether
/// consumption is outrunning the window, so every reading is paired with how
/// much of the window has elapsed.
public struct UsagePace: Hashable, Sendable {
    /// How far through the window we are, 0...1.
    public var elapsedFraction: Double
    /// How much of the allowance is gone, 0...1.
    public var usedFraction: Double
    /// used ÷ elapsed. 1.0 is exactly linear; 2.0 is burning twice as fast.
    public var ratio: Double
    /// When the allowance hits 100% at the current rate, if that lands before
    /// the window resets.
    public var projectedExhaustion: Date?

    /// Below this, the window is too young for a rate to mean anything —
    /// one request a minute in would read as an infinite burn rate.
    static let minimumElapsedFraction = 0.02

    public enum Verdict: Sendable {
        case tooEarly       // not enough of the window has passed to judge
        case comfortable    // meaningfully under pace
        case onPace
        case overspending   // will exhaust before reset
    }

    public var verdict: Verdict {
        guard elapsedFraction >= Self.minimumElapsedFraction else { return .tooEarly }
        if projectedExhaustion != nil { return .overspending }
        if ratio < 0.8 { return .comfortable }
        return .onPace
    }
}

public extension QuotaWindow {
    /// Start of the current window, derived from its reset time and length.
    func windowStart() -> Date? {
        guard let resetsAt, let windowMinutes else { return nil }
        return resetsAt.addingTimeInterval(-Double(windowMinutes) * 60)
    }

    /// Usage measured against elapsed time. Nil when the window's bounds are
    /// unknown, since without them there is no pace to speak of.
    func pace(asOf now: Date = Date()) -> UsagePace? {
        guard let resetsAt, let start = windowStart(), resetsAt > start else { return nil }

        let length = resetsAt.timeIntervalSince(start)
        let elapsed = min(max(0, now.timeIntervalSince(start)), length)
        let elapsedFraction = elapsed / length
        let usedFraction = max(0, effectiveUsedPercent(asOf: now) / 100)

        var ratio = 1.0
        var exhaustion: Date?
        if elapsedFraction >= UsagePace.minimumElapsedFraction {
            ratio = usedFraction / elapsedFraction
            if usedFraction > 0 {
                // Linear extrapolation: at this burn rate, when does it hit 100%?
                let projected = start.addingTimeInterval(elapsed / usedFraction)
                if projected < resetsAt { exhaustion = projected }
            }
        }

        return UsagePace(
            elapsedFraction: elapsedFraction,
            usedFraction: usedFraction,
            ratio: ratio,
            projectedExhaustion: exhaustion
        )
    }
}
