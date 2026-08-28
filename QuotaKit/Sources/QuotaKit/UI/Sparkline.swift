#if canImport(SwiftUI)
import SwiftUI

/// Usage trajectory across one quota window.
///
/// The horizontal axis is the *whole* window, so where the line stops shows how
/// much time is left. The dashed diagonal is linear consumption — spending the
/// allowance exactly evenly. Ink above that line means burning faster than the
/// clock, which is the one comparison that makes a usage percentage actionable.
///
/// The vertical axis is pinned to 0–100 on every instance so sparklines stacked
/// as small multiples share a scale and can be read against each other.
public struct Sparkline: View {
    public let samples: [UsageSample]
    public let window: QuotaWindow
    public let now: Date
    public var showsPaceReference: Bool
    public var lineWidth: CGFloat

    public init(
        samples: [UsageSample],
        window: QuotaWindow,
        now: Date = Date(),
        showsPaceReference: Bool = true,
        lineWidth: CGFloat = 1.5
    ) {
        self.samples = samples
        self.window = window
        self.now = now
        self.showsPaceReference = showsPaceReference
        self.lineWidth = lineWidth
    }

    private var pace: UsagePace? { window.pace(asOf: now) }

    private var tint: Color {
        Tufte.color(for: pace?.verdict ?? .tooEarly)
    }

    public var body: some View {
        GeometryReader { geometry in
            let size = geometry.size
            let plotted = points(in: size)
            // Nothing is known about the current window, so draw nothing — an
            // empty reference frame would imply a reading of zero.
            let hasReading = window.currentUsedPercent(asOf: now) != nil

            ZStack(alignment: .topLeading) {
                if hasReading {
                if showsPaceReference {
                    // Reference first, so data ink sits on top of it.
                    Path { path in
                        path.move(to: CGPoint(x: 0, y: size.height))
                        path.addLine(to: CGPoint(x: size.width, y: 0))
                    }
                    .stroke(
                        Tufte.rule,
                        style: StrokeStyle(lineWidth: 0.75, dash: [2, 2.5])
                    )
                    // Subordinate to the data: on a low-usage window the
                    // reference would otherwise out-weigh the line it exists
                    // to contextualise.
                    .opacity(0.5)
                }

                if plotted.count >= 2 {
                    Path { path in
                        path.move(to: plotted[0])
                        for point in plotted.dropFirst() { path.addLine(to: point) }
                    }
                    .stroke(tint, style: StrokeStyle(lineWidth: lineWidth, lineJoin: .round))
                }

                // Current value: the only dot, because it is the only point the
                // reader needs to locate precisely.
                if let last = plotted.last {
                    Circle()
                        .fill(tint)
                        .frame(width: 3.5, height: 3.5)
                        .position(last)
                }
                }
            }
        }
    }

    /// Maps samples into the plot rect. Falls back to the samples' own time span
    /// when the window's bounds are unknown.
    private func points(in size: CGSize) -> [CGPoint] {
        guard size.width > 0, size.height > 0 else { return [] }

        let start: Date
        let end: Date
        if let windowStart = window.windowStart(), let reset = window.resetsAt, reset > windowStart {
            start = windowStart
            end = reset
        } else if let first = samples.first?.at, let last = samples.last?.at, last > first {
            start = first
            end = last
        } else {
            // A single reading still deserves a dot at the right height.
            let used = window.effectiveUsedPercent(asOf: now)
            return [CGPoint(x: size.width / 2, y: y(used, in: size.height))]
        }

        let span = end.timeIntervalSince(start)
        guard span > 0 else { return [] }

        var series = samples.filter { $0.at >= start && $0.at <= end }
        let current = window.effectiveUsedPercent(asOf: now)
        // Always anchor the line at the present reading, even if history is thin.
        if series.last.map({ abs($0.usedPercent - current) > 0.01 || $0.at < now.addingTimeInterval(-60) }) ?? true {
            series.append(UsageSample(at: min(now, end), usedPercent: current))
        }

        return series.map { sample in
            CGPoint(
                x: size.width * CGFloat(sample.at.timeIntervalSince(start) / span),
                y: y(sample.usedPercent, in: size.height)
            )
        }
    }

    private func y(_ percent: Double, in height: CGFloat) -> CGFloat {
        let clamped = min(max(percent, 0), 100) / 100
        return height - (height * CGFloat(clamped))
    }
}
#endif
