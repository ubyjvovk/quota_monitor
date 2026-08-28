import QuotaKit
import SwiftUI
import WidgetKit

// The widget never talks to a provider. It only reads what the menu bar app
// wrote into the shared App Group container, keeping tokens and network access
// out of the sandboxed extension entirely.

struct QuotaEntry: TimelineEntry {
    let date: Date
    let snapshot: QuotaSnapshot
    let history: UsageHistory
}

struct QuotaTimelineProvider: TimelineProvider {
    private let store = SnapshotStore()

    private func current() -> (QuotaSnapshot, UsageHistory) {
        let snapshot = store.load() ?? .empty
        return (snapshot, HistoryStore(store: store).load())
    }

    func placeholder(in context: Context) -> QuotaEntry {
        QuotaEntry(date: Date(), snapshot: .preview, history: .preview)
    }

    func getSnapshot(in context: Context, completion: @escaping (QuotaEntry) -> Void) {
        if context.isPreview {
            completion(QuotaEntry(date: Date(), snapshot: .preview, history: .preview))
        } else {
            let (snapshot, history) = current()
            completion(QuotaEntry(date: Date(), snapshot: snapshot, history: history))
        }
    }

    func getTimeline(in context: Context, completion: @escaping (Timeline<QuotaEntry>) -> Void) {
        let (snapshot, history) = current()
        let start = Date()

        // The app reloads timelines whenever it refreshes; these entries only
        // keep countdowns and pace honest if the app is not running.
        let entries = stride(from: 0, through: 50, by: 10).map { minutes in
            QuotaEntry(
                date: start.addingTimeInterval(TimeInterval(minutes * 60)),
                snapshot: snapshot,
                history: history
            )
        }
        completion(Timeline(entries: entries, policy: .atEnd))
    }
}

// MARK: - Views

struct QuotaWidgetView: View {
    @Environment(\.widgetFamily) private var family
    let entry: QuotaEntry

    var body: some View {
        Group {
            if entry.snapshot.providers.allSatisfy({ $0.windows.isEmpty }) {
                emptyState
            } else if family == .systemSmall {
                SmallView(entry: entry)
            } else {
                WideView(entry: entry)
            }
        }
        .containerBackground(Tufte.background, for: .widget)
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
}

/// Small: one window — the most constrained — at full attention.
private struct SmallView: View {
    let entry: QuotaEntry

    var body: some View {
        let now = entry.date
        let headline = entry.snapshot.headline(asOf: now)

        VStack(alignment: .leading, spacing: 0) {
            if let headline {
                let used = headline.window.currentUsedPercent(asOf: now)
                let verdict = headline.window.pace(asOf: now)?.verdict ?? .tooEarly

                Text("\(headline.provider.displayName) · \(headline.window.label)")
                    .font(Tufte.meta(9.5))
                    .foregroundStyle(Tufte.textSecondary)

                Text(QuotaFormat.percentOrDash(used))
                    .font(Tufte.serif(38, .medium).monospacedDigit())
                    .foregroundStyle(verdict == .overspending ? Tufte.highlight : Tufte.text)
                    .padding(.top, 1)

                Sparkline(
                    samples: entry.history.samples(
                        provider: headline.provider.id, window: headline.window.id
                    ),
                    window: headline.window,
                    now: now
                )
                .frame(height: 26)
                .padding(.top, 4)

                Spacer(minLength: 2)

                Text(QuotaFormat.paceSummary(for: headline.window, asOf: now))
                    .font(Tufte.meta(9))
                    .foregroundStyle(verdict == .overspending ? Tufte.highlight : Tufte.textSecondary)
                    .lineLimit(2)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(QuotaFormat.finding(for: entry.snapshot, asOf: entry.date))
    }
}

/// Medium and large: small multiples across every provider and window.
private struct WideView: View {
    let entry: QuotaEntry

    var body: some View {
        let now = entry.date

        VStack(alignment: .leading, spacing: 7) {
            Text(QuotaFormat.finding(for: entry.snapshot, asOf: now))
                .font(Tufte.serif(12, .semibold))
                .foregroundStyle(Tufte.text)
                .lineLimit(2)
                .fixedSize(horizontal: false, vertical: true)

            ForEach(entry.snapshot.rankedProviders(asOf: now)) { provider in
                if !provider.windows.isEmpty {
                    VStack(alignment: .leading, spacing: 3) {
                        Text(provider.displayName)
                            .font(Tufte.meta(9.5))
                            .foregroundStyle(Tufte.textSecondary)

                        ForEach(provider.sortedWindows(asOf: now)) { window in
                            WidgetWindowRow(
                                window: window,
                                samples: entry.history.samples(
                                    provider: provider.id, window: window.id
                                ),
                                now: now
                            )
                        }
                    }
                }
            }

            Spacer(minLength: 0)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(QuotaFormat.finding(for: entry.snapshot, asOf: now))
    }
}

private struct WidgetWindowRow: View {
    let window: QuotaWindow
    let samples: [UsageSample]
    let now: Date

    var body: some View {
        let used = window.currentUsedPercent(asOf: now)
        let verdict = window.pace(asOf: now)?.verdict ?? .tooEarly

        HStack(alignment: .firstTextBaseline, spacing: 8) {
            Text(window.label)
                .font(Tufte.meta(9))
                .foregroundStyle(Tufte.textSecondary)
                .frame(width: 30, alignment: .leading)

            Sparkline(samples: samples, window: window, now: now, lineWidth: 1.3)
                .frame(height: 17)

            Text(QuotaFormat.percentOrDash(used))
                .font(Tufte.figure(12, .medium))
                .foregroundStyle(verdict == .overspending ? Tufte.highlight : Tufte.text)
                .frame(width: 34, alignment: .trailing)
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
        .description("Usage against pace across your LLM providers.")
        .supportedFamilies([.systemSmall, .systemMedium, .systemLarge])
    }
}

@main
struct QuotaWidgetBundle: WidgetBundle {
    var body: some Widget {
        QuotaWidget()
    }
}
