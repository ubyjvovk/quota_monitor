import QuotaKit
import SwiftUI
import WidgetKit

// The widget never talks to a provider and never runs the core. It only reads
// what the menu bar app wrote into the shared App Group container, keeping
// tokens, subprocesses and network access out of the sandboxed extension.

struct QuotaEntry: TimelineEntry {
    let date: Date
    let snapshot: QuotaSnapshot
}

struct QuotaTimelineProvider: TimelineProvider {
    private let store = SnapshotStore()

    private func current() -> QuotaSnapshot {
        store.load() ?? .empty
    }

    func placeholder(in context: Context) -> QuotaEntry {
        QuotaEntry(date: Date(), snapshot: .preview)
    }

    func getSnapshot(in context: Context, completion: @escaping (QuotaEntry) -> Void) {
        let snapshot: QuotaSnapshot = context.isPreview ? .preview : current()
        completion(QuotaEntry(date: Date(), snapshot: snapshot))
    }

    func getTimeline(in context: Context, completion: @escaping (Timeline<QuotaEntry>) -> Void) {
        let snapshot = current()
        let start = Date()

        // The app reloads timelines whenever it refreshes; these entries exist so
        // a window that rolls over stops showing a stale percentage while the app
        // is not running.
        let entries = stride(from: 0, through: 50, by: 10).map { minutes in
            QuotaEntry(date: start.addingTimeInterval(TimeInterval(minutes * 60)), snapshot: snapshot)
        }
        completion(Timeline(entries: entries, policy: .atEnd))
    }
}

// MARK: - Views

/// One provider's tightest current reading, ready to draw.
private struct ProviderReading: Identifiable {
    let id: String
    let displayName: String
    let shortName: String
    let usedPercent: Double
}

/// A compact readout: the tightest providers, one `shortName pct%` row each.
///
/// Small holds two rows, medium and large three — past that the type has to
/// shrink below what a glanceable widget can carry.
struct QuotaWidgetView: View {
    @Environment(\.widgetFamily) private var family
    let entry: QuotaEntry

    var body: some View {
        Group {
            if rows.isEmpty {
                emptyState
            } else {
                VStack(alignment: .leading, spacing: family == .systemSmall ? 6 : 5) {
                    ForEach(rows) { row in
                        ProviderRow(
                            shortName: row.shortName,
                            usedPercent: row.usedPercent,
                            compact: family == .systemSmall
                        )
                    }
                    Spacer(minLength: 0)
                }
                .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
            }
        }
        .containerBackground(Tufte.background, for: .widget)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(accessibilityText)
    }

    /// Providers with a current reading, tightest first, capped by family.
    ///
    /// A provider whose window has rolled over is dropped rather than shown as
    /// `0%` — the widget has no room to explain a dash.
    private var rows: [ProviderReading] {
        let now = entry.date
        let readings = entry.snapshot.rankedProviders(asOf: now)
            .compactMap { provider -> ProviderReading? in
                guard let window = provider.tightestWindow(asOf: now),
                      let used = window.currentUsedPercent(asOf: now)
                else { return nil }
                return ProviderReading(
                    id: provider.id,
                    displayName: provider.displayName,
                    shortName: provider.shortName,
                    usedPercent: used
                )
            }
        return Array(readings.prefix(family == .systemSmall ? 2 : 3))
    }

    private var emptyState: some View {
        VStack(alignment: .leading, spacing: 3) {
            Text("Quota")
                .font(Tufte.serif(13, .semibold))
                .foregroundStyle(Tufte.text)
            Text("Open Quota Monitor to load usage.")
                .font(Tufte.meta(10))
                .foregroundStyle(Tufte.textSecondary)
                .fixedSize(horizontal: false, vertical: true)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .leading)
    }

    private var accessibilityText: String {
        guard !rows.isEmpty else { return "Quota Monitor has no usage data yet" }
        return rows
            .map { "\($0.displayName) \(QuotaFormat.percent($0.usedPercent))" }
            .joined(separator: ", ")
    }
}

/// One provider: short tag, percentage, and a bar coloured by severity.
private struct ProviderRow: View {
    let shortName: String
    let usedPercent: Double
    let compact: Bool

    var body: some View {
        let severity = QuotaFormat.Severity.forUsage(usedPercent)

        VStack(alignment: .leading, spacing: 2) {
            HStack(alignment: .firstTextBaseline, spacing: 6) {
                Text(shortName)
                    .font(Tufte.meta(compact ? 10 : 9.5))
                    .foregroundStyle(Tufte.textSecondary)
                Spacer(minLength: 4)
                Text(QuotaFormat.percent(usedPercent))
                    .font(Tufte.figure(compact ? 20 : 16, .medium))
                    .foregroundStyle(severity.color)
            }

            GeometryReader { geometry in
                ZStack(alignment: .leading) {
                    Capsule()
                        .fill(Tufte.rule.opacity(0.45))
                    Capsule()
                        .fill(severity.color)
                        .frame(width: geometry.size.width * CGFloat(min(1, max(0, usedPercent / 100))))
                }
            }
            .frame(height: 3)
        }
    }
}

// MARK: - Widget

struct QuotaWidget: Widget {
    var body: some WidgetConfiguration {
        StaticConfiguration(kind: "dev.quotamonitor.widget", provider: QuotaTimelineProvider()) { entry in
            QuotaWidgetView(entry: entry)
        }
        .configurationDisplayName("Quota")
        .description("How much of each subscription is left.")
        .supportedFamilies([.systemSmall, .systemMedium, .systemLarge])
    }
}

@main
struct QuotaWidgetBundle: WidgetBundle {
    var body: some Widget {
        QuotaWidget()
    }
}
