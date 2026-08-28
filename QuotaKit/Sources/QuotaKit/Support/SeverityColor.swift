#if canImport(SwiftUI)
import SwiftUI

public extension QuotaFormat.Severity {
    /// Shared colour ramp so the menu bar, the panel and the widget agree on
    /// what "getting tight" looks like.
    var color: Color {
        switch self {
        case .normal: .green
        case .warning: .orange
        case .critical: .red
        }
    }
}

public extension QuotaWindow {
    func severity(asOf now: Date = Date()) -> QuotaFormat.Severity {
        .forUsage(effectiveUsedPercent(asOf: now))
    }

    func color(asOf now: Date = Date()) -> Color {
        severity(asOf: now).color
    }
}
#endif
