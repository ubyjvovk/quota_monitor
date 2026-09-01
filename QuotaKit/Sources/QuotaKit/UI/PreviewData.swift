#if canImport(SwiftUI)
import Foundation

public extension QuotaSnapshot {
    /// Representative data for previews and design review. Deliberately includes
    /// one window over pace, since that is the state the design must handle well.
    static var preview: QuotaSnapshot {
        let now = Date()
        return QuotaSnapshot(
            providers: [
                ProviderSnapshot(
                    id: "claude", displayName: "Claude", plan: "max",
                    windows: [
                        QuotaWindow(id: "five_hour", label: "5h", kind: .session,
                                    usedPercent: 71, resetsAt: now.addingTimeInterval(3600 * 2),
                                    windowMinutes: 300),
                        QuotaWindow(id: "seven_day", label: "Week", kind: .weekly,
                                    usedPercent: 12, resetsAt: now.addingTimeInterval(86_400 * 5),
                                    windowMinutes: 10080),
                    ],
                    observedAt: now, origin: .live
                ),
                ProviderSnapshot(
                    id: "codex", displayName: "ChatGPT", plan: "plus",
                    windows: [
                        QuotaWindow(id: "primary", label: "Week", kind: .weekly,
                                    usedPercent: 18, resetsAt: now.addingTimeInterval(86_400 * 2),
                                    windowMinutes: 10080),
                    ],
                    observedAt: now.addingTimeInterval(-7200), origin: .local
                ),
            ],
            generatedAt: now
        )
    }
}

public extension UsageHistory {
    /// Synthetic trajectories so previews show real-looking lines.
    static var preview: UsageHistory {
        let now = Date()
        var history = UsageHistory()

        func ramp(_ key: String, span: TimeInterval, to peak: Double, curve: Double, points: Int = 30) {
            history.series[key] = (0...points).map { step in
                let t = Double(step) / Double(points)
                return UsageSample(
                    at: now.addingTimeInterval(-span * (1 - t)),
                    usedPercent: peak * pow(t, curve)
                )
            }
        }
        // Front-loaded burn: climbs faster than the clock.
        ramp(UsageHistory.key(provider: "claude", window: "five_hour"),
             span: 3600 * 3, to: 71, curve: 0.65)
        ramp(UsageHistory.key(provider: "claude", window: "seven_day"),
             span: 86_400 * 2, to: 12, curve: 1.1)
        ramp(UsageHistory.key(provider: "codex", window: "primary"),
             span: 86_400 * 5, to: 18, curve: 1.3)
        return history
    }
}
#endif
