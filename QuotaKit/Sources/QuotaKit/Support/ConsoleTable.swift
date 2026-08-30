import Foundation

/// The `quotamon` console table, as data. A line-for-line port of
/// core/cmd/quotamon/table.go; the golden test pins it to the Go output, so
/// any change to the table layout must land in both files.
public enum ConsoleTable {
    /// A colour role applied to a contiguous run of console text.
    public enum Tone: Sendable, Equatable {
        /// Text rendered without a warning colour.
        case plain
        /// Text rendered with the warning colour.
        case warning
        /// Text rendered with the critical colour.
        case critical
    }

    /// A contiguous run of console text with one colour role.
    public struct Span: Sendable, Equatable {
        /// The text in this run.
        public let text: String
        /// The colour role for this run.
        public let tone: Tone
    }

    /// One rendered console line, split only where its colour role changes.
    public struct Line: Sendable, Equatable {
        /// The colour runs in display order. Provider separators have no spans.
        public let spans: [Span]
        /// The spans concatenated — what the console prints without colour.
        public var text: String { spans.map(\.text).joined() }
    }

    /// Renders one provider block per provider, in the snapshot's own order.
    /// Blocks are separated by one empty `Line`; empty input produces no lines.
    public static func render(_ providers: [ProviderSnapshot], asOf now: Date) -> [Line] {
        var lines: [Line] = []
        for provider in providers {
            if !lines.isEmpty {
                lines.append(Line(spans: []))
            }
            lines.append(contentsOf: render(provider, asOf: now))
        }
        return lines
    }

    /// Renders the block for one provider: header, windows, credits, then status.
    public static func render(_ provider: ProviderSnapshot, asOf now: Date) -> [Line] {
        let header = padTableCell(provider.displayName, width: 12) + " "
            + padTableCell(provider.plan ?? "—", width: 14) + " "
            + tableOrigin(provider.origin) + " · "
            + QuotaFormat.age(provider.age(asOf: now))
        var lines = [plainLine(header)]

        lines.append(contentsOf: provider.sortedWindows(asOf: now).map {
            renderTableWindow($0, asOf: now)
        })
        if let credits = provider.credits {
            lines.append(contentsOf: creditLines(credits).map(plainLine))
        }
        if !provider.status.isOK, let message = provider.status.message {
            lines.append(plainLine("  !  " + message))
        }
        return lines
    }

    /// Renders the uncoloured table text, byte-identical to `quotamon` for the
    /// same provider snapshots and clock value.
    public static func text(_ providers: [ProviderSnapshot], asOf now: Date) -> String {
        render(providers, asOf: now).map(\.text).joined(separator: "\n")
    }

    private static func renderTableWindow(_ window: QuotaWindow, asOf now: Date) -> Line {
        let used = window.currentUsedPercent(asOf: now)
        let percent = used.map(QuotaFormat.percent) ?? "—"
        let percentPadding = max(0, 4 - runeCount(percent))
        let filled = used.map { min(20, max(0, Int(($0 / 5).rounded()))) } ?? 0
        let tone = used.map(tableTone) ?? .plain

        var spans: [Span] = []
        append(
            "  " + truncateAndPadTableCell(window.label, width: 9) + " ",
            tone: .plain,
            to: &spans
        )
        append(String(repeating: "█", count: filled), tone: tone, to: &spans)
        append(String(repeating: "░", count: 20 - filled), tone: .plain, to: &spans)
        append(" " + String(repeating: " ", count: percentPadding), tone: .plain, to: &spans)
        append(percent, tone: used == nil ? .plain : tone, to: &spans)
        append("  " + tableCountdown(window, asOf: now), tone: .plain, to: &spans)
        return Line(spans: spans)
    }

    private static func tableCountdown(_ window: QuotaWindow, asOf now: Date) -> String {
        guard let resetsAt = window.resetsAt else { return "—" }
        let remaining = resetsAt.timeIntervalSince(now)
        guard remaining > 0 else { return "reset" }
        return QuotaFormat.countdown(remaining)
    }

    private static func padTableCell(_ value: String, width: Int) -> String {
        let padding = width - runeCount(value)
        guard padding > 0 else { return value }
        return value + String(repeating: " ", count: padding)
    }

    private static func truncateAndPadTableCell(_ value: String, width: Int) -> String {
        guard runeCount(value) > width else {
            return padTableCell(value, width: width)
        }

        var truncated = ""
        for scalar in value.unicodeScalars.prefix(width - 1) {
            truncated.unicodeScalars.append(scalar)
        }
        return padTableCell(truncated + "…", width: width)
    }

    // A distinct prepaid balance and month-to-date spend appear as two rows;
    // spend-only providers keep a single row so spend is never doubled.
    private static func creditLines(_ credits: Credits) -> [String] {
        if let spend = credits.spend, !credits.unlimited {
            var lines: [String] = []
            if let detail = tableCredits(credits) {
                lines.append("  " + padTableCell("balance", width: 9) + " " + detail)
            }
            lines.append("  " + padTableCell("spend", width: 9) + " " + spend)
            return lines
        }

        guard let detail = tableCredits(credits) else { return [] }
        let label = credits.unlimited && credits.balance != nil ? "spend" : "credits"
        return ["  " + padTableCell(label, width: 9) + " " + detail]
    }

    private static func tableCredits(_ credits: Credits) -> String? {
        if credits.unlimited {
            return credits.balance ?? "unlimited"
        }
        if credits.enabled {
            return (credits.balance ?? "—") + " remaining"
        }
        guard let balance = credits.balance,
              !disabledCreditBalanceIsEmptyOrZero(balance)
        else { return nil }
        return balance + " (not enabled)"
    }

    private static func disabledCreditBalanceIsEmptyOrZero(_ balance: String) -> Bool {
        let scalars = Array(balance.unicodeScalars)
        var start = 0
        var end = scalars.count
        while start < end, trimsDisabledBalance(scalars[start]) {
            start += 1
        }
        while start < end, trimsDisabledBalance(scalars[end - 1]) {
            end -= 1
        }

        var trimmed = ""
        for scalar in scalars[start..<end] {
            trimmed.unicodeScalars.append(scalar)
        }
        guard !trimmed.isEmpty else { return true }
        return Double(trimmed.replacingOccurrences(of: ",", with: ".")) == 0
    }

    private static func trimsDisabledBalance(_ scalar: Unicode.Scalar) -> Bool {
        scalar.properties.isWhitespace || scalar.properties.generalCategory == .currencySymbol
    }

    private static func tableOrigin(_ origin: SnapshotOrigin) -> String {
        switch origin {
        case .live: "live"
        case .local: "cached"
        case .unavailable: "unavailable"
        }
    }

    private static func tableTone(_ used: Double) -> Tone {
        switch QuotaFormat.Severity.forUsage(used) {
        case .normal: .plain
        case .warning: .warning
        case .critical: .critical
        }
    }

    private static func runeCount(_ value: String) -> Int {
        value.unicodeScalars.count
    }

    private static func plainLine(_ text: String) -> Line {
        Line(spans: [Span(text: text, tone: .plain)])
    }

    private static func append(_ text: String, tone: Tone, to spans: inout [Span]) {
        guard !text.isEmpty else { return }
        if let last = spans.last, last.tone == tone {
            spans[spans.count - 1] = Span(text: last.text + text, tone: tone)
        } else {
            spans.append(Span(text: text, tone: tone))
        }
    }
}
