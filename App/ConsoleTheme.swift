import AppKit
import SwiftUI

/// The panel's palette and metrics, transcribed from `scripts/screenshot-console.py`
/// so the menu bar panel and `docs/console.png` are the same picture.
///
/// The script draws SF Mono at 26px on a 2x canvas with `ascent + descent + 8px`
/// line height and 28px padding; every number below is that, halved to points.
/// Dark is the terminal's own palette; light is its inverse on the paper ground
/// the rest of the project uses.
enum ConsoleTheme {
    /// SF Mono 13pt — the face `scripts/screenshot-console.py` draws with.
    static let font = Font(NSFont.monospacedSystemFont(ofSize: 13, weight: .regular))

    /// Extra leading between table rows — the console's `+8px` at 2x.
    static let lineSpacing: CGFloat = 4

    /// Padding around the table — the console's 28px canvas padding at 2x.
    static let padding: CGFloat = 14

    /// Panel width: the console's 46 columns of SF Mono 13pt plus `padding` both sides.
    static let width: CGFloat = 400

    /// The panel ground: paper in light, the console's `(30, 30, 36)` in dark.
    static let background = dynamic(light: hex(0xFFFFF8), dark: hex(0x1E1E24))

    /// Table text: the console's `(222, 222, 226)` in dark, its inverse in light.
    static let text = dynamic(light: hex(0x1E1E24), dark: hex(0xDEDEE2))

    /// A window nearing its limit — the console's `(222, 180, 60)`.
    static let warning = dynamic(light: hex(0xB8860B), dark: hex(0xDEB43C))

    /// A window at its limit, and any failure message — the console's `(232, 90, 82)`.
    static let critical = dynamic(light: hex(0xC8433B), dark: hex(0xE85A52))

    /// Chrome around the table — header, footer, meta text. Quieter than the data.
    static var chrome: Color { text.opacity(0.6) }

    /// Hairlines separating chrome from table. The least ink that still separates.
    static var rule: Color { text.opacity(0.15) }

    // MARK: - Helpers

    /// Splits `0xRRGGBB` into sRGB components.
    private static func hex(_ value: UInt32) -> (Double, Double, Double) {
        (Double((value >> 16) & 0xFF) / 255,
         Double((value >> 8) & 0xFF) / 255,
         Double(value & 0xFF) / 255)
    }

    /// A colour that resolves from `NSAppearance`, not from `colorScheme`.
    ///
    /// `DesignSnapshot` flips appearance by setting `NSApp.appearance`; a colour
    /// built from the SwiftUI environment alone would stay light in both renders.
    /// Same pattern as the widget's theme — deliberately duplicated so `App/`
    /// does not depend on it.
    private static func dynamic(
        light: (Double, Double, Double),
        dark: (Double, Double, Double)
    ) -> Color {
        Color(nsColor: NSColor(name: nil) { appearance in
            let isDark = appearance.bestMatch(from: [.aqua, .darkAqua]) == .darkAqua
            let c = isDark ? dark : light
            return NSColor(srgbRed: c.0, green: c.1, blue: c.2, alpha: 1)
        })
    }
}
