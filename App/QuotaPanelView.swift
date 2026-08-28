import QuotaKit
import SwiftUI

struct QuotaPanelView: View {
    @Environment(QuotaEngine.self) private var engine
    @State private var showingSettings = false

    /// Drives countdowns and pace so the panel stays honest while open.
    @State private var now = Date()
    private let clock = Timer.publish(every: 30, on: .main, in: .common).autoconnect()

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            header

            if engine.snapshot.providers.isEmpty {
                loading
            } else {
                VStack(alignment: .leading, spacing: 16) {
                    ForEach(Array(engine.snapshot.rankedProviders(asOf: now).enumerated()), id: \.element.id) { index, provider in
                        if index > 0 {
                            // A hairline instead of a boxed card: separation with
                            // the least possible ink.
                            Rectangle()
                                .fill(Tufte.rule)
                                .frame(height: 0.5)
                                .opacity(0.6)
                        }
                        ProviderCard(provider: provider, history: engine.history, now: now)
                    }
                }
                .padding(.horizontal, 14)
                .padding(.vertical, 12)
            }

            if showingSettings { SettingsSection() }

            footer
        }
        .frame(width: 348)
        .background(Tufte.background)
        .onReceive(clock) { now = $0 }
    }

    /// States the finding, not the contents. Rule: a heading should assert.
    private var header: some View {
        HStack(alignment: .top, spacing: 8) {
            VStack(alignment: .leading, spacing: 2) {
                Text(QuotaFormat.finding(for: engine.snapshot, asOf: now))
                    .font(Tufte.serif(13, .semibold))
                    .foregroundStyle(Tufte.text)
                    .fixedSize(horizontal: false, vertical: true)
                Text(subtitle)
                    .font(Tufte.meta(9.5))
                    .foregroundStyle(Tufte.textSecondary)
            }

            Spacer(minLength: 4)

            Button {
                Task { await engine.refresh() }
            } label: {
                if engine.isRefreshing {
                    ProgressView().controlSize(.small)
                } else {
                    Image(systemName: "arrow.clockwise").font(.system(size: 11))
                }
            }
            .buttonStyle(.plain)
            .disabled(engine.isRefreshing)
            .help("Refresh now")
            .accessibilityLabel("Refresh now")

            Button {
                showingSettings.toggle()
            } label: {
                Image(systemName: "slider.horizontal.3").font(.system(size: 11))
            }
            .buttonStyle(.plain)
            .help("Settings")
            .accessibilityLabel("Settings")
        }
        .foregroundStyle(Tufte.textSecondary)
        .padding(.horizontal, 14)
        .padding(.top, 12)
        .padding(.bottom, 10)
    }

    private var subtitle: String {
        guard let last = engine.lastRefreshedAt else { return "never refreshed" }
        return "updated \(QuotaFormat.age(now.timeIntervalSince(last)))"
    }

    private var loading: some View {
        HStack(spacing: 8) {
            ProgressView().controlSize(.small)
            Text("Reading providers…")
                .font(Tufte.meta(10))
                .foregroundStyle(Tufte.textSecondary)
        }
        .padding(14)
    }

    private var footer: some View {
        VStack(spacing: 0) {
            Rectangle().fill(Tufte.rule).frame(height: 0.5).opacity(0.6)
            HStack {
                Button("Quit") { NSApplication.shared.terminate(nil) }
                    .buttonStyle(.plain)
                    .font(Tufte.meta(10))
                    .foregroundStyle(Tufte.textSecondary)
                Spacer()
                Text("v\(Bundle.main.shortVersion)")
                    .font(Tufte.meta(9))
                    .foregroundStyle(Tufte.textSecondary)
            }
            .padding(.horizontal, 14)
            .padding(.vertical, 8)
        }
    }
}

struct SettingsSection: View {
    @Environment(QuotaEngine.self) private var engine

    private static let intervals: [(String, TimeInterval)] = [
        ("1m", 60), ("5m", 300), ("15m", 900), ("30m", 1800),
    ]

    var body: some View {
        @Bindable var engine = engine

        VStack(alignment: .leading, spacing: 8) {
            Rectangle().fill(Tufte.rule).frame(height: 0.5).opacity(0.6)

            VStack(alignment: .leading, spacing: 7) {
                Text("Live refresh")
                    .font(Tufte.meta(9))
                    .foregroundStyle(Tufte.textSecondary)

                // Off means local files only: no network call, no token read.
                ForEach([Claude.providerID, Codex.providerID], id: \.self) { id in
                    Toggle(isOn: liveBinding(for: id)) {
                        Text(id == Claude.providerID ? Claude.displayName : Codex.displayName)
                            .font(Tufte.meta(10.5))
                            .foregroundStyle(Tufte.text)
                    }
                    .toggleStyle(.switch)
                    .controlSize(.mini)
                }

                HStack(spacing: 6) {
                    Text("Every")
                        .font(Tufte.meta(10.5))
                        .foregroundStyle(Tufte.text)
                    Picker("", selection: $engine.settings.refreshInterval) {
                        ForEach(Self.intervals, id: \.1) { Text($0.0).tag($0.1) }
                    }
                    .labelsHidden()
                    .controlSize(.small)
                    .frame(width: 78)
                }

                if !engine.isSharedWithWidget {
                    Text("Widget sharing off — no App Group container.")
                        .font(Tufte.meta(9))
                        .foregroundStyle(Tufte.textSecondary)
                }
            }
            .padding(.horizontal, 14)
            .padding(.vertical, 11)
        }
    }

    private func liveBinding(for id: String) -> Binding<Bool> {
        Binding(
            get: { engine.settings.isLiveEnabled(id) },
            set: { engine.settings.liveEnabled[id] = $0 }
        )
    }
}

extension Bundle {
    var shortVersion: String {
        object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String ?? "0"
    }
}
