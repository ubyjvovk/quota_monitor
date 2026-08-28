import QuotaKit
import SwiftUI

@main
struct QuotaMonitorApp: App {
    @NSApplicationDelegateAdaptor(AppDelegate.self) private var delegate
    @State private var engine = QuotaEngine()

    var body: some Scene {
        MenuBarExtra {
            QuotaPanelView()
                .environment(engine)
        } label: {
            // Rendered persistently in the status bar, so this is where the
            // refresh loop is started and kept alive.
            Text(QuotaFormat.menuBarTitle(for: engine.snapshot))
                .task { engine.startAutoRefresh() }
        }
        .menuBarExtraStyle(.window)
    }
}
