import AppKit
import QuotaKit
import SwiftUI

/// Renders the panel to PNG so the design can be reviewed without a screenshot.
///
/// Set `QUOTA_MONITOR_RENDER=/path/prefix` to write `<prefix>-light.png` and
/// `<prefix>-dark.png`, then exit. Development aid only; a normal launch never
/// touches this.
enum DesignSnapshot {

    @MainActor
    static func exportIfRequested() {
        guard let prefix = ProcessInfo.processInfo.environment["QUOTA_MONITOR_RENDER"] else { return }

        let useRealData = ProcessInfo.processInfo.environment["QUOTA_MONITOR_RENDER_REAL"] != nil
        let engine = useRealData ? QuotaEngine() : QuotaEngine(preview: .preview)

        for (name, appearance) in [("light", NSAppearance.Name.aqua), ("dark", .darkAqua)] {
            guard let nsAppearance = NSAppearance(named: appearance) else { continue }

            // Tufte's colours come from NSAppearance, so the whole app has to
            // adopt the appearance; a SwiftUI colorScheme override alone leaves
            // them resolving light.
            NSApp.appearance = nsAppearance
            NSAppearance.current = nsAppearance

            let renderer = ImageRenderer(
                content: QuotaPanelView()
                    .environment(engine)
                    .environment(\.colorScheme, appearance == .darkAqua ? .dark : .light)
                    .frame(width: 348)
            )
            renderer.scale = 2
            let image = renderer.nsImage

            guard let image,
                  let tiff = image.tiffRepresentation,
                  let bitmap = NSBitmapImageRep(data: tiff),
                  let png = bitmap.representation(using: .png, properties: [:])
            else { continue }

            try? png.write(to: URL(fileURLWithPath: "\(prefix)-\(name).png"))
        }

        NSApplication.shared.terminate(nil)
    }
}

final class AppDelegate: NSObject, NSApplicationDelegate {
    func applicationDidFinishLaunching(_ notification: Notification) {
        MainActor.assumeIsolated { DesignSnapshot.exportIfRequested() }
    }
}
