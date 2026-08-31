#if canImport(SwiftUI) && canImport(AppKit)
import Foundation
import SwiftUI

/// The widget's face: console rows in SF Mono Light, one per provider, sized
/// to the widget family. Lives in QuotaKit so the app can render it headlessly
/// (the widget itself needs an App Group to be installed).
public struct ConsoleWidgetView: View {

    /// Which widget family the rows are cut for. `small` and `medium` are one
    /// condensed row per provider; `large` is the whole console table.
    /// Widths are the bar rows' budget. A credits row and the ` *` staleness
    /// marker can run a little past it; `minimumScaleFactor` takes up the slack,
    /// and losing the marker to save two cells would be the wrong trade.
    public enum Size: Sendable {
        /// `systemSmall` — 17 columns: short name, an 8-cell bar, the percentage.
        case small
        /// `systemMedium` — 37 columns: the console's 20-cell bar plus a countdown.
        case medium
        /// `systemLarge` — the console table verbatim, in snapshot order.
        case large
    }

    /// The snapshot to draw, the clock to age it against, and the family to fit.
    public init(snapshot: QuotaSnapshot, asOf now: Date, size: Size) {
        self.snapshot = snapshot
        self.now = now
        self.size = size
    }

    private let snapshot: QuotaSnapshot
    private let now: Date
    private let size: Size

    /// The rows this view draws, as console lines — the testable part.
    ///
    /// `small` and `medium` list providers tightest-first (a widget has no room
    /// to make the user hunt): first every provider with a current reading, then
    /// every provider that has no window at all but does report a balance. A
    /// window that has rolled over is still dropped rather than shown, because
    /// there is no room to explain a dash. `large` is `ConsoleTable.render`
    /// unchanged.
    ///
    /// `nonisolated`, and so is every helper below it: `View` is a
    /// `@MainActor @preconcurrency` protocol, so conforming makes *every* member
    /// of this type main-actor isolated, static ones included. Without this the
    /// `compactMap` closures below inherit that isolation and trap the process
    /// — silently, no message — the moment a parallel test calls it off the main
    /// thread. Line building is pure text; it belongs to no actor.
    public nonisolated static func lines(
        for snapshot: QuotaSnapshot,
        asOf now: Date,
        size: Size
    ) -> [ConsoleTable.Line] {
        if size == .large {
            return ConsoleTable.render(snapshot.providers, asOf: now)
        }

        let ranked = snapshot.rankedProviders(asOf: now)
        var lines = ranked.compactMap { provider -> ConsoleTable.Line? in
            guard let window = provider.tightestWindow(asOf: now),
                  let used = window.currentUsedPercent(asOf: now)
            else { return nil }
            return size == .small
                ? smallLine(provider, used: used)
                : mediumLine(provider, window: window, used: used, asOf: now)
        }

        // Credits-only providers (DeepInfra) come after the bars rather than in
        // the ranking: they have no percentage to rank by, and keeping them
        // together stops a text row from splitting the bar column. Without this
        // block they were invisible in the condensed families — a widget showing
        // only DeepInfra told the user to open the app.
        lines.append(contentsOf: ranked.compactMap { provider -> ConsoleTable.Line? in
            guard provider.tightestWindow(asOf: now) == nil,
                  let credits = provider.credits,
                  let detail = creditsDetail(credits)
            else { return nil }
            return creditsLine(provider, detail: detail)
        })
        return lines
    }

    public var body: some View {
        Group {
            if lines.isEmpty {
                Text("Open Quota Monitor to load usage.")
                    .font(ConsoleTheme.font)
                    .foregroundStyle(ConsoleTheme.chrome)
                    .fixedSize(horizontal: false, vertical: true)
            } else {
                // One Text holding one AttributedString, not a stack of rows: a
                // single run of monospaced text is what makes the columns line up
                // and the `█░` bars join. It also means `minimumScaleFactor`
                // shrinks every line by the same factor, so the medium and large
                // columns fit the family width without breaking alignment.
                Text(attributed)
                    .font(ConsoleTheme.font)
                    .lineSpacing(ConsoleTheme.lineSpacing)
                    .multilineTextAlignment(.leading)
                    .lineLimit(lines.count)
                    .minimumScaleFactor(0.7)
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        .padding(ConsoleTheme.padding)
    }

    /// The lines for this view's snapshot, clock and family.
    private var lines: [ConsoleTable.Line] {
        Self.lines(for: snapshot, asOf: now, size: size)
    }

    /// The lines joined by newlines, each span carrying its tone's colour.
    private var attributed: AttributedString {
        var result = AttributedString()
        for (index, line) in lines.enumerated() {
            if index > 0 { result += AttributedString("\n") }
            for span in line.spans {
                var run = AttributedString(span.text)
                run.foregroundColor = Self.color(for: span.tone)
                result += run
            }
        }
        return result
    }

    /// The console's three colours, one per tone.
    private static func color(for tone: ConsoleTable.Tone) -> Color {
        switch tone {
        case .plain: ConsoleTheme.text
        case .warning: ConsoleTheme.warning
        case .critical: ConsoleTheme.critical
        }
    }

    // MARK: - Row layout

    /// `GPT ████████ 100%` — 17 columns. The bar is scaled to eight cells
    /// rather than the console's twenty; there is no room for more.
    private nonisolated static func smallLine(_ provider: ProviderSnapshot, used: Double) -> ConsoleTable.Line {
        let filled = min(8, max(0, Int((used / 100 * 8).rounded())))
        let tone = tone(for: used)

        var spans: [ConsoleTable.Span] = []
        append(pad(provider.shortName, width: 3) + " ", tone: .plain, to: &spans)
        append(String(repeating: "█", count: filled), tone: tone, to: &spans)
        append(String(repeating: "░", count: 8 - filled), tone: .plain, to: &spans)
        appendPercent(used, to: &spans)
        appendStaleness(provider, to: &spans)
        return ConsoleTable.Line(spans: spans)
    }

    /// `GPT ████████████████████ 100%  8m` — 37 columns at most. The bar is the
    /// console's own twenty-cell rule, so a medium widget and the terminal draw
    /// the same picture of the same window.
    private nonisolated static func mediumLine(
        _ provider: ProviderSnapshot,
        window: QuotaWindow,
        used: Double,
        asOf now: Date
    ) -> ConsoleTable.Line {
        let filled = min(20, max(0, Int((used / 5).rounded())))
        let tone = tone(for: used)
        // A surviving row's reset is always in the future, so a nil here means
        // the window never told us when it resets — not that it has passed.
        let countdown = window.timeUntilReset(asOf: now).map(QuotaFormat.countdown) ?? "—"

        var spans: [ConsoleTable.Span] = []
        append(pad(provider.shortName, width: 3) + " ", tone: .plain, to: &spans)
        append(String(repeating: "█", count: filled), tone: tone, to: &spans)
        append(String(repeating: "░", count: 20 - filled), tone: .plain, to: &spans)
        appendPercent(used, to: &spans)
        append("  " + countdown, tone: .plain, to: &spans)
        appendStaleness(provider, to: &spans)
        return ConsoleTable.Line(spans: spans)
    }

    /// `DI  $10.03 remaining` — a provider that reports no window at all, only a
    /// balance. The short name is padded like every other row so the rows still
    /// line up under one another; the detail is the console's own credits text.
    private nonisolated static func creditsLine(
        _ provider: ProviderSnapshot,
        detail: String
    ) -> ConsoleTable.Line {
        var spans: [ConsoleTable.Span] = []
        append(pad(provider.shortName, width: 3) + " " + detail, tone: .plain, to: &spans)
        appendStaleness(provider, to: &spans)
        return ConsoleTable.Line(spans: spans)
    }

    /// Appends ` *` to any row whose reading is not a healthy live one.
    ///
    /// The console spells this out per provider as `cached · 2d ago` and an `!`
    /// status line; seventeen cells have room for neither, and without *some*
    /// marker a two-day-old cached 41% is indistinguishable from a live 41% —
    /// the user reads stale headroom as real headroom. One trailing glyph is the
    /// cheapest honest signal; the panel and the large family still explain why.
    private nonisolated static func appendStaleness(
        _ provider: ProviderSnapshot,
        to spans: inout [ConsoleTable.Span]
    ) {
        guard provider.origin != .live || !provider.status.isOK else { return }
        append(" *", tone: .plain, to: &spans)
    }

    /// The console's credits text for one provider — the detail half of the
    /// table's `credits`/`balance` row.
    ///
    /// A transcription of `ConsoleTable.tableCredits`, which is private to that
    /// type; the two must change together. `nil` means there is nothing worth
    /// showing, which is how a switched-off zero balance disappears instead of
    /// being read as available headroom.
    private nonisolated static func creditsDetail(_ credits: Credits) -> String? {
        if credits.unlimited {
            return credits.balance ?? "unlimited"
        }
        if credits.enabled {
            return (credits.balance ?? "—") + " remaining"
        }
        guard let balance = credits.balance, !isEmptyOrZero(balance) else { return nil }
        return balance + " (not enabled)"
    }

    /// Whether a switched-off balance is worth no row at all: blank, or zero
    /// once currency symbols and spacing are stripped.
    private nonisolated static func isEmptyOrZero(_ balance: String) -> Bool {
        let scalars = Array(balance.unicodeScalars)
        var start = 0
        var end = scalars.count
        while start < end, isBalancePadding(scalars[start]) { start += 1 }
        while start < end, isBalancePadding(scalars[end - 1]) { end -= 1 }

        var trimmed = ""
        for scalar in scalars[start..<end] {
            trimmed.unicodeScalars.append(scalar)
        }
        guard !trimmed.isEmpty else { return true }
        return Double(trimmed.replacingOccurrences(of: ",", with: ".")) == 0
    }

    private nonisolated static func isBalancePadding(_ scalar: Unicode.Scalar) -> Bool {
        scalar.properties.isWhitespace || scalar.properties.generalCategory == .currencySymbol
    }

    /// A leading space, then the percentage right-aligned in four cells. Only
    /// the digits carry the tone; the padding stays plain.
    private nonisolated static func appendPercent(_ used: Double, to spans: inout [ConsoleTable.Span]) {
        let percent = QuotaFormat.percent(used)
        let padding = max(0, 4 - percent.unicodeScalars.count)
        append(" " + String(repeating: " ", count: padding), tone: .plain, to: &spans)
        append(percent, tone: tone(for: used), to: &spans)
    }

    /// The same mapping `ConsoleTable` uses: normal usage draws no colour at all.
    private nonisolated static func tone(for used: Double) -> ConsoleTable.Tone {
        switch QuotaFormat.Severity.forUsage(used) {
        case .normal: .plain
        case .warning: .warning
        case .critical: .critical
        }
    }

    /// Pads to a fixed column count, measured in scalars like the console's own
    /// padding so a multi-byte short name still occupies three cells.
    private nonisolated static func pad(_ value: String, width: Int) -> String {
        let padding = width - value.unicodeScalars.count
        guard padding > 0 else { return value }
        return value + String(repeating: " ", count: padding)
    }

    /// Appends text, merging into the previous span when the tone is unchanged
    /// so a line is split only where its colour actually changes.
    private nonisolated static func append(
        _ text: String,
        tone: ConsoleTable.Tone,
        to spans: inout [ConsoleTable.Span]
    ) {
        guard !text.isEmpty else { return }
        if let last = spans.last, last.tone == tone {
            spans[spans.count - 1] = ConsoleTable.Span(text: last.text + text, tone: tone)
        } else {
            spans.append(ConsoleTable.Span(text: text, tone: tone))
        }
    }
}
#endif
