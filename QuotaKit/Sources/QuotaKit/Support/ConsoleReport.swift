import Foundation

/// Renders resolved snapshots as plain text. A pure function of its inputs —
/// no I/O — so it is unit-testable without network or Keychain.
public enum ConsoleReport {

    /// One provider block each, windows in `sortedWindows()` order.
    ///
    /// A window with no current reading renders `—`, never `0.0%`. A missing
    /// plan is left blank rather than printed as `nil` or a dash.
    public static func render(_ snapshots: [ProviderSnapshot], asOf: Date) -> String {
        snapshots.map { renderProvider($0, asOf: asOf) }.joined(separator: "\n")
    }

    /// Stable pretty-printed JSON keyed by provider id.
    ///
    /// Each provider object carries `plan`, `origin`, `observed_at` (ISO-8601),
    /// `status`, and a `windows` array of `{id, label, used_percent, resets_at}`.
    /// `used_percent` is JSON `null` when the window has no current reading.
    public static func renderJSON(_ snapshots: [ProviderSnapshot], asOf: Date) throws -> String {
        var payload: [String: ProviderPayload] = [:]
        payload.reserveCapacity(snapshots.count)
        for snapshot in snapshots {
            payload[snapshot.id] = ProviderPayload(snapshot, asOf: asOf)
        }
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.sortedKeys, .prettyPrinted]
        encoder.dateEncodingStrategy = .iso8601
        let data = try encoder.encode(payload)
        guard let text = String(data: data, encoding: .utf8) else {
            throw QuotaError.malformed("console JSON was not valid UTF-8")
        }
        return text
    }

    // MARK: - Text

    private static func renderProvider(_ snapshot: ProviderSnapshot, asOf now: Date) -> String {
        var lines: [String] = [header(snapshot, asOf: now)]

        for window in snapshot.sortedWindows(asOf: now) {
            lines.append(windowLine(window, asOf: now))
        }

        if let credits = snapshot.credits {
            lines.append(creditsLine(credits))
        }

        if let message = snapshot.status.message {
            lines.append("  \(message)")
        }

        return lines.joined(separator: "\n") + "\n"
    }

    private static func header(_ snapshot: ProviderSnapshot, asOf now: Date) -> String {
        let name = padded(snapshot.displayName, 16)
        let plan = snapshot.plan ?? ""
        return padded(name + plan, 52) + originTag(snapshot, asOf: now)
    }

    private static func originTag(_ snapshot: ProviderSnapshot, asOf now: Date) -> String {
        let label = snapshot.origin.displayName
        if snapshot.origin == .unavailable {
            return label
        }
        return "\(label) · \(QuotaFormat.age(snapshot.age(asOf: now)))"
    }

    private static func windowLine(_ window: QuotaWindow, asOf now: Date) -> String {
        let label = padded(window.label, 16)
        guard let used = window.currentUsedPercent(asOf: now) else {
            return "  \(label)—  no reading since this window reset"
        }
        let percent = String(format: "%.1f%%", used)
        guard let remaining = window.timeUntilReset(asOf: now) else {
            return "  \(label)\(percent)"
        }
        return "  \(label)\(percent)  resets in \(QuotaFormat.countdown(remaining))"
    }

    private static func creditsLine(_ credits: Credits) -> String {
        let detail: String
        if credits.unlimited {
            detail = "unlimited"
        } else if let balance = credits.balance {
            detail = "\(balance) remaining"
        } else {
            detail = "— remaining"
        }
        return "  \(padded("credits", 16))\(detail)"
    }

    private static func padded(_ text: String, _ width: Int) -> String {
        text.count >= width ? text : text + String(repeating: " ", count: width - text.count)
    }

    // MARK: - JSON payload

    /// Provider object emitted by `renderJSON`. Keys always present, even when
    /// the value is JSON `null` — Swift's synthesized `Encodable` would omit them.
    private struct ProviderPayload: Encodable {
        var plan: String?
        var origin: String
        var observed_at: Date
        var status: String?
        var windows: [WindowPayload]

        init(_ snapshot: ProviderSnapshot, asOf now: Date) {
            self.plan = snapshot.plan
            self.origin = snapshot.origin.rawValue
            self.observed_at = snapshot.observedAt
            self.status = snapshot.status.message
            self.windows = snapshot.sortedWindows(asOf: now).map { WindowPayload($0, asOf: now) }
        }

        enum CodingKeys: String, CodingKey {
            case plan, origin, observed_at, status, windows
        }

        func encode(to encoder: Encoder) throws {
            var container = encoder.container(keyedBy: CodingKeys.self)
            try container.encode(plan, forKey: .plan)
            try container.encode(origin, forKey: .origin)
            try container.encode(observed_at, forKey: .observed_at)
            try container.encode(status, forKey: .status)
            try container.encode(windows, forKey: .windows)
        }
    }

    private struct WindowPayload: Encodable {
        var id: String
        var label: String
        var used_percent: Double?
        var resets_at: Date?

        init(_ window: QuotaWindow, asOf now: Date) {
            self.id = window.id
            self.label = window.label
            self.used_percent = window.currentUsedPercent(asOf: now)
            self.resets_at = window.resetsAt
        }

        enum CodingKeys: String, CodingKey {
            case id, label, used_percent, resets_at
        }

        func encode(to encoder: Encoder) throws {
            var container = encoder.container(keyedBy: CodingKeys.self)
            try container.encode(id, forKey: .id)
            try container.encode(label, forKey: .label)
            try container.encode(used_percent, forKey: .used_percent)
            try container.encode(resets_at, forKey: .resets_at)
        }
    }
}
