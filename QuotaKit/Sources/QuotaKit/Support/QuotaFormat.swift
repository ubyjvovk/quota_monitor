import Foundation

/// Presentation helpers shared by the menu bar app and the widget, so both
/// render the same numbers the same way.
public enum QuotaFormat {

    /// How alarming a usage level is. Drives colour in every surface.
    public enum Severity: Sendable {
        case normal, warning, critical

        public static func forUsage(_ percent: Double) -> Severity {
            switch percent {
            case ..<70: .normal
            case ..<90: .warning
            default: .critical
            }
        }
    }

    public static func percent(_ value: Double) -> String {
        "\(Int(value.rounded()))%"
    }

    /// Compact countdown: "6d 7h", "10h 11m", "42m", "<1m".
    public static func countdown(_ interval: TimeInterval) -> String {
        let total = max(0, Int(interval))
        let days = total / 86_400
        let hours = (total % 86_400) / 3600
        let minutes = (total % 3600) / 60
        if days > 0 { return "\(days)d \(hours)h" }
        if hours > 0 { return "\(hours)h \(minutes)m" }
        if minutes > 0 { return "\(minutes)m" }
        return "<1m"
    }

    /// Compact age: "just now", "20s ago", "3h ago", "2d ago".
    public static func age(_ interval: TimeInterval) -> String {
        let total = max(0, Int(interval))
        if total < 5 { return "just now" }
        if total < 60 { return "\(total)s ago" }
        if total < 3600 { return "\(total / 60)m ago" }
        if total < 86_400 { return "\(total / 3600)h ago" }
        return "\(total / 86_400)d ago"
    }

    /// One-line summary of a window: "5h · 43% · resets in 10h 11m".
    public static func windowSummary(_ window: QuotaWindow, asOf now: Date = Date()) -> String {
        var parts = [window.label, percent(window.effectiveUsedPercent(asOf: now))]
        if let remaining = window.timeUntilReset(asOf: now) {
            parts.append("resets in \(countdown(remaining))")
        }
        return parts.joined(separator: " · ")
    }

    /// The whole menu bar title, e.g. "CL 43% · GPT 18%".
    /// Providers with no reading show a dash so absence never reads as zero.
    public static func menuBarTitle(for snapshot: QuotaSnapshot, asOf now: Date = Date()) -> String {
        let parts = snapshot.providers.map { provider -> String in
            guard let window = provider.tightestWindow(asOf: now),
                  let used = window.currentUsedPercent(asOf: now)
            else { return "\(provider.shortName) –" }
            return "\(provider.shortName) \(percent(used))"
        }
        return parts.isEmpty ? "Quota –" : parts.joined(separator: "  ·  ")
    }
}
