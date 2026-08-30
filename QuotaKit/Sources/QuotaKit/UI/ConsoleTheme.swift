#if canImport(SwiftUI) && canImport(AppKit)
import AppKit
import SwiftUI

/// The console palette and metrics, transcribed from `scripts/screenshot-console.py`
/// so the menu bar panel, the widget and `docs/console.png` are the same picture.
///
/// The script draws SF Mono at 26px on a 2x canvas with `ascent + descent + 8px`
/// line height and 28px padding; every number below is that, halved to points.
/// Dark is the terminal's own palette; light is its inverse on the paper ground
/// the rest of the project uses.
public enum ConsoleTheme {
    /// SF Mono 13pt **Light** — the face `scripts/screenshot-console.py` draws with.
    ///
    /// Light, not regular: `/System/Library/Fonts/SFNSMono.ttf` is a variable font
    /// whose default instance sits at weight axis 294 (Light), and the terminal
    /// screenshot is drawn at that default. Asking for `.regular` here made the
    /// app visibly heavier than the console — measured as 961 vs 573 ink pixels
    /// in the same word.
    public static let font = Font(NSFont.monospacedSystemFont(ofSize: 13, weight: .light))

    /// Extra leading between table rows — the console's `+8px` at 2x.
    public static let lineSpacing: CGFloat = 4

    /// Padding around the table — the console's 28px canvas padding at 2x.
    public static let padding: CGFloat = 14

    /// Panel width: the console's 46 columns of SF Mono 13pt plus `padding` both sides.
    public static let width: CGFloat = 400

    /// The panel ground: paper in light, the console's `(30, 30, 36)` in dark.
    public static let background = dynamic(light: hex(0xFFFFF8), dark: hex(0x1E1E24))

    /// Table text: the console's `(222, 222, 226)` in dark, its inverse in light.
    public static let text = dynamic(light: hex(0x1E1E24), dark: hex(0xDEDEE2))

    /// A window nearing its limit — the console's `(222, 180, 60)`.
    public static let warning = dynamic(light: hex(0xB8860B), dark: hex(0xDEB43C))

    /// A window at its limit, and any failure message — the console's `(232, 90, 82)`.
    public static let critical = dynamic(light: hex(0xC8433B), dark: hex(0xE85A52))

    /// Chrome around the table — header, footer, meta text. Quieter than the data.
    public static var chrome: Color { text.opacity(0.6) }

    /// Hairlines separating chrome from table. The least ink that still separates.
    public static var rule: Color { text.opacity(0.15) }

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
    /// `NSColor(name:)` resolves the same way inside the widget extension.
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
#endif
