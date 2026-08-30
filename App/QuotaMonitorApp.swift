import QuotaKit
import SwiftUI

@main
struct QuotaMonitorApp: App {
    @NSApplicationDelegateAdaptor(AppDelegate.self) private var delegate

    /// Fed entirely by the bundled Go core. With no bundled binary the engine has
    /// no source at all, and the panel says the build is broken.
    @State private var engine = QuotaEngine(runner: CoreBinary.runner)

    /// nil when the core is missing, which also hides the setup pane — there is
    /// nothing to configure without it.
    private let setup = CoreBinary.runner.map(QuotamonSetup.init(runner:))

    var body: some Scene {
        MenuBarExtra {
            QuotaPanelView(setup: setup)
                .environment(engine)
        } label: {
            // Rendered persistently in the status bar, so this is where the
            // refresh loop is started and kept alive.
            Text(QuotaFormat.menuBarTitle(for: engine.snapshot))
                .task {
                    // The screenshot path exits immediately; starting a refresh
                    // loop there would spawn a subprocess it never waits for.
                    guard !DesignSnapshot.isRendering else { return }
                    engine.startAutoRefresh()
                }
        }
        .menuBarExtraStyle(.window)
    }
}
