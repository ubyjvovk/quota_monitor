import QuotaKit
import SwiftUI

/// First-run configuration: what the core found locally, and which providers to
/// turn on.
///
/// Nothing here talks to a provider — `quotamon discover` only looks at local
/// credential files and (on macOS) whether the Claude Keychain item exists. Save
/// writes one `quotamon config set` per provider and then asks the engine for a
/// fresh reading.
struct SetupView: View {
    let setup: QuotamonSetup
    /// Called after the config has been written, so the panel can refresh.
    let onFinish: () -> Void
    /// Called when the user backs out without saving.
    let onCancel: (() -> Void)?

    @State private var findings: [SetupFinding] = []
    @State private var enabled: [String: Bool] = [:]
    /// What the config file said when this pane opened — the baseline Save
    /// compares against, so an untouched provider is never rewritten.
    @State private var saved: [String: SavedProvider] = [:]
    @State private var keys: [String: String] = [:]
    @State private var error: String?
    @State private var isLoading = true
    @State private var isSaving = false

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            Text("Choose providers")
                .font(ConsoleTheme.font)
                .foregroundStyle(ConsoleTheme.text)

            Text("Quota Monitor looked for credentials each provider's CLI already writes. Nothing was contacted.")
                .font(ConsoleTheme.font)
                .foregroundStyle(ConsoleTheme.chrome)
                .fixedSize(horizontal: false, vertical: true)

            if isLoading {
                HStack(spacing: 8) {
                    ProgressView().controlSize(.small)
                    Text("Looking…")
                        .font(ConsoleTheme.font)
                        .foregroundStyle(ConsoleTheme.chrome)
                }
                .padding(.vertical, 4)
            } else {
                ForEach(findings) { finding in
                    row(for: finding)
                }
            }

            if let error {
                Text(error)
                    .font(ConsoleTheme.font)
                    .foregroundStyle(ConsoleTheme.critical)
                    .fixedSize(horizontal: false, vertical: true)
            }

            HStack(spacing: 8) {
                if let onCancel {
                    Button("Cancel", action: onCancel)
                        .controlSize(.small)
                        .disabled(isSaving)
                }
                Spacer()
                Button("Save") { Task { await save() } }
                    .controlSize(.small)
                    .keyboardShortcut(.defaultAction)
                    .disabled(isLoading || isSaving)
            }
            .padding(.top, 2)
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 12)
        .task { await load() }
    }

    private func row(for finding: SetupFinding) -> some View {
        VStack(alignment: .leading, spacing: 3) {
            HStack(alignment: .firstTextBaseline, spacing: 6) {
                Text(mark(for: finding))
                    .font(ConsoleTheme.font)
                    .foregroundStyle(finding.found ? ConsoleTheme.text : ConsoleTheme.chrome)
                    .frame(width: 12, alignment: .leading)

                if finding.supported {
                    Toggle(isOn: binding(for: finding.id)) {
                        Text(finding.displayName)
                            .font(ConsoleTheme.font)
                            .foregroundStyle(ConsoleTheme.text)
                    }
                    .toggleStyle(.switch)
                    .controlSize(.mini)
                } else {
                    Text(finding.displayName)
                        .font(ConsoleTheme.font)
                        .foregroundStyle(ConsoleTheme.chrome)
                }

                Spacer(minLength: 0)
            }

            Text(finding.found ? finding.detail : "\(finding.detail) — \(finding.hint)")
                .font(ConsoleTheme.font)
                .foregroundStyle(ConsoleTheme.chrome)
                .fixedSize(horizontal: false, vertical: true)
                .padding(.leading, 18)

            if finding.needsKey, enabled[finding.id] == true {
                // Bound to memory only; the value goes straight to
                // `quotamon config set`, which writes the file 0600.
                SecureField("API key", text: keyBinding(for: finding.id))
                    .textFieldStyle(.roundedBorder)
                    .controlSize(.small)
                    .padding(.leading, 18)
            }
        }
    }

    /// ✓ found, ✗ looked and did not find, · not supported by this build.
    private func mark(for finding: SetupFinding) -> String {
        guard finding.supported else { return "·" }
        return finding.found ? "✓" : "✗"
    }

    private func binding(for id: String) -> Binding<Bool> {
        Binding(get: { enabled[id] ?? false }, set: { enabled[id] = $0 })
    }

    private func keyBinding(for id: String) -> Binding<String> {
        Binding(get: { keys[id] ?? "" }, set: { keys[id] = $0 })
    }

    private func load() async {
        isLoading = true
        defer { isLoading = false }
        do {
            let discovered = try await setup.discover()
            let stored = try await setup.savedProviders()
            findings = discovered
            saved = stored ?? [:]
            // A saved choice wins over discovery. Discovery only looks at the
            // usual credential paths, so a provider the user enabled by hand at
            // an unusual one comes back "not found" — starting it toggled off
            // and then saving would silently switch it back off. On a first run
            // there is nothing saved, and the default is on for what was found:
            // the common case is "yes, use what I already signed into".
            enabled = Dictionary(
                uniqueKeysWithValues: discovered.map { finding in
                    (finding.id, stored?[finding.id]?.enabled ?? (finding.supported && finding.found))
                }
            )
        } catch {
            self.error = error.localizedDescription
        }
    }

    private func save() async {
        isSaving = true
        defer { isSaving = false }
        do {
            for finding in findings where finding.supported {
                let choice = enabled[finding.id] ?? false
                let key = finding.needsKey ? (keys[finding.id] ?? "") : ""
                // Only providers the user actually changed are written. A no-op
                // Save must not rewrite the file: every write is a chance to
                // clobber something this pane never knew about.
                guard saved[finding.id]?.enabled != choice || !key.isEmpty else { continue }
                try await setup.save(
                    id: finding.id,
                    enabled: choice,
                    apiKey: key.isEmpty ? nil : key
                )
            }
        } catch {
            self.error = error.localizedDescription
            return
        }
        onFinish()
    }
}
