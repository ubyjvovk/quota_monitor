import AppKit
import QuotaKit

/// The status-bar glyph: one thin horizontal bar per provider, stacked into a
/// single compact icon, each filled to that provider's tightest-window usage and
/// coloured by severity. A provider with no current reading shows an empty track.
///
/// This replaces a text title so the menu bar stays icon-sized no matter how many
/// providers are configured.
enum MenuBarIcon {

    @MainActor
    static func image(for snapshot: QuotaSnapshot, now: Date = Date()) -> NSImage {
        let providers = snapshot.rankedProviders(asOf: now)
        let size = NSSize(width: 20, height: 15)

        let image = NSImage(size: size, flipped: false) { rect in
            let count = max(providers.count, 1)
            let gap: CGFloat = count > 1 ? 1 : 0
            let rowHeight = (rect.height - gap * CGFloat(count - 1)) / CGFloat(count)
            let radius = min(rowHeight / 2, 1.5)

            for index in 0..<count {
                // Top provider (most constrained) draws first, at the top.
                let y = rect.height - CGFloat(index + 1) * rowHeight - CGFloat(index) * gap
                let track = NSRect(x: 0, y: y, width: rect.width, height: rowHeight)

                NSColor.tertiaryLabelColor.setFill()
                NSBezierPath(roundedRect: track, xRadius: radius, yRadius: radius).fill()

                guard index < providers.count,
                      let window = providers[index].tightestWindow(asOf: now),
                      let used = window.currentUsedPercent(asOf: now)
                else { continue }

                let fraction = min(max(used / 100, 0), 1)
                guard fraction > 0 else { continue }
                let fill = NSRect(x: 0, y: y, width: rect.width * fraction, height: rowHeight)
                color(forUsage: used).setFill()
                NSBezierPath(roundedRect: fill, xRadius: radius, yRadius: radius).fill()
            }
            return true
        }
        // Coloured, not a template — severity colour is the point.
        image.isTemplate = false
        image.accessibilityDescription = QuotaFormat.menuBarTitle(for: snapshot, asOf: now)
        return image
    }

    private static func color(forUsage percent: Double) -> NSColor {
        switch QuotaFormat.Severity.forUsage(percent) {
        case .normal: return .systemGreen
        case .warning: return .systemOrange
        case .critical: return .systemRed
        }
    }
}
