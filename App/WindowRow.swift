import QuotaKit
import SwiftUI

/// One quota window: trajectory, figure, verdict.
///
/// Replaces the progress bar it used to be. A bar encodes a single number in a
/// lot of ink and cannot answer "compared to what?"; the sparkline carries the
/// same number plus its history and its pace reference in less space.
struct WindowRow: View {
    let provider: ProviderSnapshot
    let window: QuotaWindow
    let samples: [UsageSample]
    let now: Date

    private var used: Double? { window.currentUsedPercent(asOf: now) }
    private var verdict: UsagePace.Verdict { window.pace(asOf: now)?.verdict ?? .tooEarly }

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            HStack(alignment: .firstTextBaseline, spacing: 10) {
                Text(window.label)
                    .font(Tufte.meta(10))
                    .foregroundStyle(Tufte.textSecondary)
                    .frame(width: 34, alignment: .leading)

                Sparkline(samples: samples, window: window, now: now)
                    .frame(height: 24)
                    .padding(.bottom, 1)

                Text(QuotaFormat.percentOrDash(used))
                    .font(Tufte.figure(16, .medium))
                    .foregroundStyle(verdict == .overspending ? Tufte.highlight : Tufte.text)
                    .frame(width: 42, alignment: .trailing)
            }

            Text(QuotaFormat.paceSummary(for: window, asOf: now))
                .font(Tufte.meta(9.5))
                .foregroundStyle(verdict == .overspending ? Tufte.highlight : Tufte.textSecondary)
                .padding(.leading, 44)
        }
        // The chart is decorative to VoiceOver; the sentence carries the meaning.
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(QuotaFormat.accessibleDescription(provider: provider, window: window, asOf: now))
    }
}
