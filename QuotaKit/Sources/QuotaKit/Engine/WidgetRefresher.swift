import Foundation

#if canImport(WidgetKit)
import WidgetKit
#endif

/// Nudges WidgetKit after a new snapshot lands, so the desktop widget follows the
/// menu bar rather than waiting for its own timeline.
public enum WidgetRefresher {
    public static func reloadAll() {
        #if canImport(WidgetKit)
        WidgetCenter.shared.reloadAllTimelines()
        #endif
    }
}
