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

/// The console table, cut to the widget family: `ConsoleWidgetView` does the
/// drawing so the app's headless render path can show exactly what the widget
/// shows (installing the widget itself needs an App Group this machine lacks).
struct QuotaWidgetView: View {
    @Environment(\.widgetFamily) private var family
    let entry: QuotaEntry

    var body: some View {
        ConsoleWidgetView(snapshot: entry.snapshot, asOf: entry.date, size: size)
            .containerBackground(ConsoleTheme.background, for: .widget)
            .accessibilityElement(children: .ignore)
            .accessibilityLabel(accessibilityText)
    }

    /// The widget family as a row layout. Anything unexpected gets the medium
    /// rows, which fit any family at least as wide as `systemMedium`.
    private var size: ConsoleWidgetView.Size {
        switch family {
        case .systemSmall: .small
        case .systemLarge: .large
        default: .medium
        }
    }

    /// The drawn rows read aloud, so VoiceOver hears the same numbers the
    /// columns show.
    private var accessibilityText: String {
        let rows = ConsoleWidgetView.lines(for: entry.snapshot, asOf: entry.date, size: size)
            .map(\.text)
            .filter { !$0.isEmpty }
        guard !rows.isEmpty else { return "Quota Monitor has no usage data yet" }
        return rows.joined(separator: ", ")
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
