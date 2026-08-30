import Foundation
import QuotaKit
import SwiftUI

/// One provider block, laid out like the console table: a header line, one row
/// per window, then credits.
///
/// Deliberately plain — no sparklines, no pace verdicts. The console is the
/// reference rendering and this mirrors it, so the two surfaces can never
/// disagree about what a number means.
struct ProviderSection: View {
    let provider: ProviderSnapshot
    let now: Date

    var body: some View {
        VStack(alignment: .leading, spacing: 5) {
            header

            ForEach(provider.sortedWindows(asOf: now)) { window in
                WindowRow(window: window, now: now)
            }

            if let credits = provider.credits, let line = Self.creditsLine(credits) {
                LabelledRow(label: line.label, detail: line.detail)
            }

            // A cached number still shows — but never silently as though live.
            if let message = provider.status.message {
                Text(message)
                    .font(Tufte.meta(9.5))
                    .foregroundStyle(Tufte.textSecondary)
                    .fixedSize(horizontal: false, vertical: true)
                    .padding(.top, 1)
            }
        }
    }

    /// `DisplayName · plan · origin · age`, with absent parts simply left out.
    private var header: some View {
        HStack(alignment: .firstTextBaseline, spacing: 6) {
            Text(provider.displayName)
                .font(Tufte.serif(12.5, .semibold))
                .foregroundStyle(Tufte.text)

            Spacer(minLength: 4)

            Text(metaLine)
                .font(Tufte.meta(9))
                .foregroundStyle(Tufte.textSecondary)
        }
    }

    private var metaLine: String {
        var parts: [String] = []
        if let plan = provider.plan, !plan.isEmpty { parts.append(plan.lowercased()) }
        parts.append(provider.origin.displayName)
        // An unavailable provider has no reading, so "3h ago" would date nothing.
        if provider.origin != .unavailable {
            parts.append(QuotaFormat.age(provider.age(asOf: now)))
        }
        return parts.joined(separator: " · ")
    }

    /// The console's credit rules, applied to what the snapshot contract carries.
    ///
    /// A disabled balance that is absent or zero is omitted entirely: showing
    /// "$0.00" next to "credits" reads as headroom the user does not have.
    static func creditsLine(_ credits: Credits) -> (label: String, detail: String)? {
        if credits.unlimited {
            // A provider with no cap reports what it has spent, not what is left.
            guard let balance = credits.balance else { return ("credits", "unlimited") }
            return ("spend", balance)
        }
        if credits.enabled {
            guard let balance = credits.balance else { return ("credits", "— remaining") }
            return ("credits", "\(balance) remaining")
        }
        guard let balance = credits.balance, !isEmptyOrZero(balance) else { return nil }
        return ("credits", "\(balance) (not enabled)")
    }

    /// True for "", "$0", "0.00" and friends — currency symbols and spaces are
    /// stripped before the number is read.
    private static func isEmptyOrZero(_ balance: String) -> Bool {
        let strippable = CharacterSet.whitespacesAndNewlines.union(.symbols)
        let text = balance.trimmingCharacters(in: strippable)
        if text.isEmpty { return true }
        return Double(text.replacingOccurrences(of: ",", with: ".")) == 0
    }
}

/// One quota window: label, bar, percentage, reset countdown.
private struct WindowRow: View {
    let window: QuotaWindow
    let now: Date

    var body: some View {
        let used = window.currentUsedPercent(asOf: now)

        HStack(alignment: .center, spacing: 8) {
            Text(window.label)
                .font(Tufte.meta(10))
                .foregroundStyle(Tufte.textSecondary)
                .lineLimit(1)
                .frame(width: 46, alignment: .leading)

            QuotaBar(usedPercent: used, severity: used.map(QuotaFormat.Severity.forUsage))

            // Dash, never 0%: a window that reset since the reading was taken is
            // unknown, not empty.
            Text(QuotaFormat.percentOrDash(used))
                .font(Tufte.figure(12))
                .foregroundStyle(Tufte.text)
                .frame(width: 38, alignment: .trailing)

            Text(countdown)
                .font(Tufte.meta(9.5))
                .foregroundStyle(Tufte.textSecondary)
                .lineLimit(1)
                .frame(maxWidth: .infinity, alignment: .trailing)
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(
            "\(window.label) window, \(QuotaFormat.percentOrDash(used)) used, resets \(countdown)"
        )
    }

    private var countdown: String {
        guard let remaining = window.timeUntilReset(asOf: now) else {
            return window.resetsAt == nil ? "—" : "reset"
        }
        return QuotaFormat.countdown(remaining)
    }
}

/// A credits or spend row, aligned with the window labels above it.
private struct LabelledRow: View {
    let label: String
    let detail: String

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            Text(label)
                .font(Tufte.meta(10))
                .foregroundStyle(Tufte.textSecondary)
                .frame(width: 46, alignment: .leading)
            Text(detail)
                .font(Tufte.meta(10))
                .foregroundStyle(Tufte.text)
            Spacer(minLength: 0)
        }
    }
}
