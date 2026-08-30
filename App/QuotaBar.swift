import QuotaKit
import SwiftUI

/// The console's usage bar, drawn instead of typed.
///
/// A fixed track width rather than a `GeometryReader`: the panel is a fixed
/// width, and `ImageRenderer` (the screenshot path) measures a fixed frame
/// reliably where a proposed-size geometry read can come back zero.
struct QuotaBar: View {
    /// Current usage, or nil when the window reset after the reading was taken.
    let usedPercent: Double?
    /// How alarming that usage is; nil usage draws an empty track.
    let severity: QuotaFormat.Severity?

    static let trackWidth: CGFloat = 92
    private static let height: CGFloat = 4

    var body: some View {
        ZStack(alignment: .leading) {
            Capsule()
                .fill(Tufte.rule.opacity(0.45))
                .frame(width: Self.trackWidth, height: Self.height)

            if let usedPercent, let severity {
                Capsule()
                    .fill(severity.color)
                    .frame(width: Self.trackWidth * fraction(of: usedPercent), height: Self.height)
            }
        }
        .frame(width: Self.trackWidth, height: Self.height)
    }

    /// Clamped so a provider reporting overage renders a full bar, not one that
    /// overflows the row.
    private func fraction(of percent: Double) -> CGFloat {
        CGFloat(min(1, max(0, percent / 100)))
    }
}
