import AppKit
import QuotaKit
import SwiftUI

/// Renders the panel to PNG so the design can be reviewed without a screenshot.
///
/// Set `QUOTA_MONITOR_RENDER=/path/prefix` to write `<prefix>-light.png` and
/// `<prefix>-dark.png`, then exit. It runs headlessly, needs no display, and
/// always draws `sample` — never the machine's real quota, so the output is safe
/// to publish and identical on every run.
enum DesignSnapshot {

    /// Whether this launch exists only to write PNGs and quit.
    static var isRendering: Bool {
        ProcessInfo.processInfo.environment["QUOTA_MONITOR_RENDER"] != nil
    }

    @MainActor
    static func exportIfRequested() {
        guard let prefix = ProcessInfo.processInfo.environment["QUOTA_MONITOR_RENDER"] else { return }

        let engine = QuotaEngine(preview: sample)

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
                    .frame(width: 320)
            )
            renderer.scale = 2

            guard let image = renderer.nsImage,
                  let tiff = image.tiffRepresentation,
                  let bitmap = NSBitmapImageRep(data: tiff),
                  let png = bitmap.representation(using: .png, properties: [:])
            else { continue }

            try? png.write(to: URL(fileURLWithPath: "\(prefix)-\(name).png"))
        }

        NSApplication.shared.terminate(nil)
    }

    /// Invented data covering every case the panel must handle: a window near its
    /// limit, one that has rolled over (rendered `—`, never `0%`), a cached
    /// provider explaining itself, a switched-off credit balance, and a
    /// pay-as-you-go provider with spend but no windows.
    static var sample: QuotaSnapshot {
        let now = Date()
        return QuotaSnapshot(
            providers: [
                ProviderSnapshot(
                    id: "claude", displayName: "Claude", plan: "max",
                    windows: [
                        QuotaWindow(id: "seven_day_opus", label: "Opus wk", kind: .weekly,
                                    usedPercent: 93, resetsAt: now.addingTimeInterval(86_400 * 4),
                                    windowMinutes: 10080),
                        QuotaWindow(id: "five_hour", label: "5h", kind: .session,
                                    usedPercent: 71, resetsAt: now.addingTimeInterval(3600 * 2),
                                    windowMinutes: 300),
                        QuotaWindow(id: "seven_day", label: "Week", kind: .weekly,
                                    usedPercent: 24, resetsAt: now.addingTimeInterval(86_400 * 4),
                                    windowMinutes: 10080),
                    ],
                    credits: Credits(hasCredits: true, unlimited: false, balance: "$5.00", enabled: false),
                    observedAt: now, origin: .live
                ),
                ProviderSnapshot(
                    id: "codex", displayName: "ChatGPT", plan: "plus",
                    windows: [
                        QuotaWindow(id: "primary", label: "5h", kind: .session,
                                    usedPercent: 46, resetsAt: now.addingTimeInterval(3600 * 4),
                                    windowMinutes: 300),
                        QuotaWindow(id: "secondary", label: "Week", kind: .weekly,
                                    usedPercent: 62, resetsAt: now.addingTimeInterval(-3600),
                                    windowMinutes: 10080),
                    ],
                    observedAt: now.addingTimeInterval(-7200), origin: .local,
                    status: .needsSetup("Cached — live refresh failed: run `codex login`")
                ),
                ProviderSnapshot(
                    id: "grok", displayName: "Grok", plan: "supergrok",
                    windows: [
                        QuotaWindow(id: "credits", label: "Week", kind: .weekly,
                                    usedPercent: 12, resetsAt: now.addingTimeInterval(86_400 * 3),
                                    windowMinutes: 10080),
                    ],
                    credits: Credits(hasCredits: true, unlimited: false, balance: "$24.10", enabled: true),
                    observedAt: now, origin: .live
                ),
                ProviderSnapshot(
                    id: "deepinfra", displayName: "DeepInfra",
                    credits: Credits(hasCredits: false, unlimited: true, balance: "$7.75 this month", enabled: true),
                    observedAt: now, origin: .live
                ),
            ],
            generatedAt: now
        )
    }
}

final class AppDelegate: NSObject, NSApplicationDelegate {
    func applicationDidFinishLaunching(_ notification: Notification) {
        MainActor.assumeIsolated { DesignSnapshot.exportIfRequested() }
    }
}
