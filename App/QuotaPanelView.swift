import QuotaKit
import SwiftUI

/// The menu bar panel: the console table, plus the two controls a menu bar app
/// needs — refresh now, and reconfigure.
///
/// Every number comes from the bundled `quotamon` via `QuotaEngine`; this view
/// only lays it out.
struct QuotaPanelView: View {
    /// nil in the screenshot render path and in previews, where nothing shells out.
    var setup: QuotamonSetup?

    @Environment(QuotaEngine.self) private var engine

    /// Drives the reset countdowns so they stay honest while the panel is open.
    @State private var now = Date()
    @State private var showingSetup = false
    @State private var checkedForFirstRun = false

    private let clock = Timer.publish(every: 30, on: .main, in: .common).autoconnect()

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            header

            if let setup, showingSetup {
                SetupView(
                    setup: setup,
                    onFinish: {
                        showingSetup = false
                        Task { await engine.refresh(fresh: true) }
                    },
                    onCancel: cancelSetup
                )
            } else {
                table
            }

            footer
        }
        .frame(width: ConsoleTheme.width)
        .background(ConsoleTheme.background)
        .onReceive(clock) { now = $0 }
        .task { await detectFirstRun() }
    }

    /// No way out of setup until there is a table to go back to.
    private var cancelSetup: (() -> Void)? {
        guard !engine.snapshot.providers.isEmpty else { return nil }
        return { showingSetup = false }
    }

    // MARK: - Header

    private var header: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack(spacing: 8) {
                Text("Quota Monitor")
                    .font(ConsoleTheme.font)
                    .foregroundStyle(ConsoleTheme.chrome)

                Spacer(minLength: 0)

                if engine.isRefreshing {
                    ProgressView().controlSize(.small)
                }

                Button {
                    // fresh: true — the user asked *now*, so skip the core's caches.
                    Task { await engine.refresh(fresh: true) }
                } label: {
                    Image(systemName: "arrow.clockwise").font(.system(size: 12))
                }
                .buttonStyle(.plain)
                .foregroundStyle(ConsoleTheme.chrome)
                .disabled(engine.isRefreshing || CoreBinary.url == nil)
                .help("Refresh now")
                .accessibilityLabel("Refresh now")
            }
            .padding(.horizontal, 14)
            .padding(.top, 10)
            .padding(.bottom, 8)

            Rectangle().fill(ConsoleTheme.rule).frame(height: 1)
        }
    }

    // MARK: - Table

    @ViewBuilder
    private var table: some View {
        if engine.snapshot.providers.isEmpty {
            empty
        } else {
            // Snapshot order, not most-constrained-first: the console does not
            // re-rank, and the whole point of this panel is that it matches.
            ConsoleTableView(
                lines: ConsoleTable.render(engine.snapshot.providers, asOf: now)
            )
            .padding(ConsoleTheme.padding)
        }
    }

    private var empty: some View {
        HStack(spacing: 8) {
            if engine.isRefreshing { ProgressView().controlSize(.small) }
            Text(engine.isRefreshing ? "Reading providers…" : "No readings yet.")
                .font(ConsoleTheme.font)
                .foregroundStyle(ConsoleTheme.chrome)
        }
        .padding(ConsoleTheme.padding)
    }

    // MARK: - Footer

    private var footer: some View {
        VStack(alignment: .leading, spacing: 0) {
            Rectangle().fill(ConsoleTheme.rule).frame(height: 1)

            if let message = warning {
                Text(message)
                    .font(ConsoleTheme.font)
                    .foregroundStyle(ConsoleTheme.critical)
                    .fixedSize(horizontal: false, vertical: true)
                    .padding(.horizontal, 14)
                    .padding(.top, 7)
            }

            HStack(spacing: 0) {
                if setup != nil {
                    footerButton("Settings", "gearshape", help: "Choose providers") {
                        showingSetup.toggle()
                    }
                    Spacer(minLength: 0)
                }

                footerButton("About", "info.circle") { AppAbout.show() }

                Spacer(minLength: 0)

                footerButton("Quit", "power") { NSApplication.shared.terminate(nil) }
            }
            .padding(.horizontal, 14)
            .padding(.vertical, 8)
        }
    }

    /// One evenly-spaced footer action: icon + label, uniform styling.
    private func footerButton(
        _ title: String,
        _ icon: String,
        help: String? = nil,
        action: @escaping () -> Void
    ) -> some View {
        Button(action: action) {
            Label(title, systemImage: icon).font(ConsoleTheme.font)
        }
        .buttonStyle(.plain)
        .foregroundStyle(ConsoleTheme.chrome)
        .help(help ?? title)
        .accessibilityLabel(title)
    }

    /// A missing binary is a build fault and outranks any refresh error, which
    /// would only be a symptom of it.
    private var warning: String? {
        if CoreBinary.url == nil { return CoreBinary.missingMessage }
        return engine.lastError
    }

    // MARK: - First run

    /// Shows setup when there is nothing to show and the core has no config yet.
    ///
    /// Both conditions matter: an empty snapshot alone can just mean the first
    /// refresh has not landed, and a missing config alone cannot happen while
    /// readings exist.
    private func detectFirstRun() async {
        guard let setup, !checkedForFirstRun else { return }
        checkedForFirstRun = true
        guard engine.snapshot.providers.isEmpty else { return }

        if engine.lastError == QuotamonSetup.notConfiguredMessage {
            showingSetup = true
            return
        }
        showingSetup = await setup.configIsMissing()
    }
}
