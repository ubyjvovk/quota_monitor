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

            // ConsoleTheme's colours come from NSAppearance, so the whole app has
            // to adopt the appearance; a SwiftUI colorScheme override alone leaves
            // them resolving light.
            NSApp.appearance = nsAppearance
            NSAppearance.current = nsAppearance

            let renderer = ImageRenderer(
                // A non-nil setup so the Settings gear renders; its runner is
                // never invoked in a static snapshot (the sample data is non-empty,
                // so first-run detection short-circuits before shelling out).
                content: QuotaPanelView(setup: QuotamonSetup(runner: QuotamonRunner { _ in Data() }))
                    .environment(engine)
                    .environment(\.colorScheme, appearance == .darkAqua ? .dark : .light)
                    .frame(width: ConsoleTheme.width)
            )
            renderer.scale = 2

            guard let image = renderer.nsImage,
                  let tiff = image.tiffRepresentation,
                  let bitmap = NSBitmapImageRep(data: tiff),
                  let png = bitmap.representation(using: .png, properties: [:])
            else { continue }

            try? png.write(to: URL(fileURLWithPath: "\(prefix)-\(name).png"))

            // The status-bar icon, scaled up so it is legible in docs.
            let icon = MenuBarIcon.image(for: engine.snapshot)
            let scale: CGFloat = 8
            let big = NSImage(size: NSSize(width: icon.size.width * scale,
                                           height: icon.size.height * scale))
            big.lockFocus()
            NSGraphicsContext.current?.imageInterpolation = .none
            icon.draw(in: NSRect(origin: .zero, size: big.size))
            big.unlockFocus()
            if let itiff = big.tiffRepresentation,
               let ibitmap = NSBitmapImageRep(data: itiff),
               let ipng = ibitmap.representation(using: .png, properties: [:]) {
                try? ipng.write(to: URL(fileURLWithPath: "\(prefix)-menubar-\(name).png"))
            }
        }

        NSApplication.shared.terminate(nil)
    }

    /// The core's own demo snapshot, transcribed from `core/cmd/quotamon/demo.go`.
    ///
    /// Deliberately the same five providers, windows and offsets `quotamon --demo`
    /// prints, so `docs/app-*.png` and `docs/console.png` show the same numbers
    /// and any drift between the two renderings is visible at a glance. Keep this
    /// in step with `demoSnapshot` when that changes.
    static var sample: QuotaSnapshot {
        let now = Date()
        let minute: TimeInterval = 60
        let hour: TimeInterval = 3600
        let day: TimeInterval = 86_400
        // The panel renders a moment after `sample` is built, with its own `now`,
        // so an exact offset has already lost a few milliseconds by the time the
        // countdown truncates it — 40h becomes "1d 15h". Go's demo renders at
        // exactly `base` and never loses the unit; 30s of slack buys the same.
        let slack: TimeInterval = 30
        return QuotaSnapshot(
            providers: [
                ProviderSnapshot(
                    id: "claude", displayName: "Claude", plan: "max",
                    windows: [
                        QuotaWindow(id: "session", label: "5h", kind: .session,
                                    usedPercent: 6, resetsAt: now.addingTimeInterval(2 * hour + 39 * minute + slack),
                                    windowMinutes: 300),
                        QuotaWindow(id: "weekly_all", label: "Week", kind: .weekly,
                                    usedPercent: 15, resetsAt: now.addingTimeInterval(40 * hour + slack),
                                    windowMinutes: 10080),
                        QuotaWindow(id: "weekly_scoped", label: "Fable wk", kind: .weekly,
                                    usedPercent: 23, resetsAt: now.addingTimeInterval(40 * hour + slack),
                                    windowMinutes: 10080),
                    ],
                    // Balance present but not spendable — rendered "(not enabled)".
                    credits: Credits(hasCredits: true, unlimited: false, balance: "20.00", enabled: false),
                    observedAt: now, origin: .live
                ),
                ProviderSnapshot(
                    id: "codex", displayName: "ChatGPT", plan: "plus",
                    windows: [
                        QuotaWindow(id: "session", label: "5h", kind: .session,
                                    usedPercent: 100, resetsAt: now.addingTimeInterval(8 * minute + slack),
                                    windowMinutes: 300),
                        QuotaWindow(id: "weekly", label: "Week", kind: .weekly,
                                    usedPercent: 31, resetsAt: now.addingTimeInterval(5 * day + 11 * hour + slack),
                                    windowMinutes: 10080),
                    ],
                    observedAt: now, origin: .live
                ),
                ProviderSnapshot(
                    id: "grok", displayName: "Grok",
                    windows: [
                        QuotaWindow(id: "weekly", label: "Week", kind: .weekly,
                                    usedPercent: 63, resetsAt: now.addingTimeInterval(2 * day + 13 * hour + slack),
                                    windowMinutes: 10080),
                    ],
                    observedAt: now, origin: .live
                ),
                ProviderSnapshot(
                    id: "deepinfra", displayName: "DeepInfra", plan: "pay-as-you-go",
                    credits: Credits(hasCredits: true, unlimited: false, balance: "$10.03",
                                     enabled: true, spend: "$8.00 this month"),
                    observedAt: now, origin: .live
                ),
                ProviderSnapshot(
                    id: "kimi", displayName: "Kimi", plan: "basic",
                    windows: [
                        QuotaWindow(id: "session", label: "5h", kind: .session,
                                    usedPercent: 42, resetsAt: now.addingTimeInterval(3 * hour + 12 * minute + slack),
                                    windowMinutes: 300),
                        QuotaWindow(id: "weekly", label: "Week", kind: .weekly,
                                    usedPercent: 14, resetsAt: now.addingTimeInterval(4 * day + 6 * hour + slack),
                                    windowMinutes: 10080),
                    ],
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
