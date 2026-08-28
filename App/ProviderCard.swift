import QuotaKit
import SwiftUI

/// One provider: its name, where the reading came from, and a small multiple
/// per quota window — all windows on a shared 0–100 scale so they compare.
struct ProviderCard: View {
    let provider: ProviderSnapshot
    let history: UsageHistory
    let now: Date

    var body: some View {
        VStack(alignment: .leading, spacing: 9) {
            header

            if provider.windows.isEmpty {
                Text(provider.status.message ?? "No data")
                    .font(Tufte.meta(10))
                    .foregroundStyle(Tufte.textSecondary)
                    .fixedSize(horizontal: false, vertical: true)
            } else {
                ForEach(provider.sortedWindows(asOf: now)) { window in
                    WindowRow(
                        provider: provider,
                        window: window,
                        samples: history.samples(provider: provider.id, window: window.id),
                        now: now
                    )
                }
            }

            // A cached number still shows — but never silently as though live.
            if !provider.windows.isEmpty, let message = provider.status.message {
                Text(message)
                    .font(Tufte.meta(9))
                    .foregroundStyle(Tufte.textSecondary)
                    .lineLimit(2)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
    }

    private var header: some View {
        HStack(alignment: .firstTextBaseline, spacing: 6) {
            Text(provider.displayName)
                .font(Tufte.serif(13, .semibold))
                .foregroundStyle(Tufte.text)

            if let plan = provider.plan {
                Text(plan.lowercased())
                    .font(Tufte.meta(9))
                    .foregroundStyle(Tufte.textSecondary)
            }

            Spacer()

            Text(originText)
                .font(Tufte.meta(9))
                .foregroundStyle(Tufte.textSecondary)
                .help(originHelp)
        }
    }

    private var originText: String {
        switch provider.origin {
        case .live: "live"
        case .local: QuotaFormat.age(provider.age(asOf: now))
        case .unavailable: "—"
        }
    }

    private var originHelp: String {
        switch provider.origin {
        case .live: "Fetched from the provider just now"
        case .local: "Read from local CLI files, \(QuotaFormat.age(provider.age(asOf: now)))"
        case .unavailable: provider.status.message ?? "No data source available"
        }
    }
}
