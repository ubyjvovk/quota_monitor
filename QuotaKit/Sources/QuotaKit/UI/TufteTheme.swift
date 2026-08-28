#if canImport(SwiftUI)
import SwiftUI

#if canImport(AppKit)
import AppKit
#elseif canImport(UIKit)
import UIKit
#endif

/// Tufte palette and type scale, shared by the panel and the widget so both
/// surfaces render identically.
///
/// Dark is designed, not inverted: saturation drops because a colour tuned for
/// an off-white ground vibrates against a dark one.
public enum Tufte {

    public static let background = dynamic(light: hex(0xFFFFF8), dark: hex(0x151515))
    public static let text = dynamic(light: hex(0x111111), dark: hex(0xDDDDDD))
    public static let textSecondary = dynamic(light: hex(0x666666), dark: hex(0x999999))
    public static let rule = dynamic(light: hex(0xCCCCCC), dark: hex(0x444444))
    /// Data ink default. Colour is reserved for what matters.
    public static let series = dynamic(light: hex(0x666666), dark: hex(0x999999))
    /// The single accent, used only to mark trouble.
    public static let highlight = dynamic(light: hex(0xE41A1C), dark: hex(0xFC8D62))

    // MARK: Type

    /// Serif for data, per Tufte. `.serif` resolves to New York on Apple platforms.
    public static func serif(_ size: CGFloat, _ weight: Font.Weight = .regular) -> Font {
        .system(size: size, weight: weight, design: .serif)
    }

    /// Sans is permitted only for small axis/meta labels.
    public static func meta(_ size: CGFloat, _ weight: Font.Weight = .regular) -> Font {
        .system(size: size, weight: weight)
    }

    /// Figures that must align in a column.
    public static func figure(_ size: CGFloat, _ weight: Font.Weight = .regular) -> Font {
        .system(size: size, weight: weight, design: .serif).monospacedDigit()
    }

    // MARK: Semantics

    /// Colour follows the verdict, not the raw percentage: 80% used is fine near
    /// the end of a window and alarming near the start.
    public static func color(for verdict: UsagePace.Verdict) -> Color {
        switch verdict {
        case .overspending: highlight
        case .onPace, .comfortable, .tooEarly: series
        }
    }

    // MARK: Helpers

    private static func hex(_ value: UInt32) -> (Double, Double, Double) {
        (Double((value >> 16) & 0xFF) / 255,
         Double((value >> 8) & 0xFF) / 255,
         Double(value & 0xFF) / 255)
    }

    private static func dynamic(
        light: (Double, Double, Double),
        dark: (Double, Double, Double)
    ) -> Color {
        #if canImport(AppKit)
        return Color(nsColor: NSColor(name: nil) { appearance in
            let isDark = appearance.bestMatch(from: [.aqua, .darkAqua]) == .darkAqua
            let c = isDark ? dark : light
            return NSColor(srgbRed: c.0, green: c.1, blue: c.2, alpha: 1)
        })
        #elseif canImport(UIKit)
        return Color(uiColor: UIColor { traits in
            let c = traits.userInterfaceStyle == .dark ? dark : light
            return UIColor(red: c.0, green: c.1, blue: c.2, alpha: 1)
        })
        #else
        return Color(red: light.0, green: light.1, blue: light.2)
        #endif
    }
}
#endif
