import Foundation

public extension QuotaFormat {

    /// Plain-language verdict for one window, e.g. "2.1× pace · out in 3h 12m".
    ///
    /// This is the sentence a percentage cannot say on its own.
    static func paceSummary(for window: QuotaWindow, asOf now: Date = Date()) -> String {
        guard window.currentUsedPercent(asOf: now) != nil else {
            return "no reading since this window reset"
        }
        guard let pace = window.pace(asOf: now) else {
            // No window bounds, so nothing to compare against — say only what is known.
            guard let remaining = window.timeUntilReset(asOf: now) else { return "" }
            return "resets in \(countdown(remaining))"
        }

        switch pace.verdict {
        case .tooEarly:
            return "window just reset"

        case .comfortable, .onPace:
            let label = pace.verdict == .comfortable ? "under pace" : "on pace"
            guard let remaining = window.timeUntilReset(asOf: now) else { return label }
            return "\(label) · resets in \(countdown(remaining))"

        case .overspending:
            let multiple = String(format: "%.1f", pace.ratio)
            guard let exhaustion = pace.projectedExhaustion else { return "\(multiple)× pace" }
            return "\(multiple)× pace · out in \(countdown(exhaustion.timeIntervalSince(now)))"
        }
    }

    /// The single most important thing true right now, across every provider.
    /// Used as the panel and widget heading.
    static func finding(for snapshot: QuotaSnapshot, asOf now: Date = Date()) -> String {
        let overspending = snapshot.providers
            .flatMap { provider in provider.windows.map { (provider, $0) } }
            .compactMap { pair -> (ProviderSnapshot, QuotaWindow, Date)? in
                guard let exhaustion = pair.1.pace(asOf: now)?.projectedExhaustion else { return nil }
                return (pair.0, pair.1, exhaustion)
            }
            .min { $0.2 < $1.2 }

        if let (provider, window, exhaustion) = overspending {
            return "\(provider.displayName) \(window.label) runs out in \(countdown(exhaustion.timeIntervalSince(now)))"
        }

        guard let headline = snapshot.headline(asOf: now),
              let used = headline.window.currentUsedPercent(asOf: now)
        else { return "No current usage data" }
        return "All windows on pace · \(headline.provider.displayName) highest at \(percent(used))"
    }

    /// Spoken description of one window, for VoiceOver.
    static func accessibleDescription(
        provider: ProviderSnapshot,
        window: QuotaWindow,
        asOf now: Date = Date()
    ) -> String {
        let used = percent(window.effectiveUsedPercent(asOf: now))
        let summary = paceSummary(for: window, asOf: now)
        return "\(provider.displayName), \(window.label) window, \(used) used\(summary.isEmpty ? "" : ", \(summary)")"
    }
}

public extension QuotaFormat {
    /// Percentage, or an em dash when there is no current reading.
    /// Never substitutes 0% for missing data.
    static func percentOrDash(_ value: Double?) -> String {
        value.map(percent) ?? "—"
    }
}
