import QuotaKit
import SwiftUI

/// The `quotamon` table, drawn exactly as the terminal draws it.
///
/// One `Text` holding one `AttributedString`, not a stack of rows: a single run
/// of monospaced text is what makes the columns line up, the `█░` bars join, and
/// the whole block selectable and copyable the way terminal output is. The lines
/// come from `ConsoleTable` — this view only colours and places them.
struct ConsoleTableView: View {
    /// The rendered console lines, in order. Provider separators are empty lines.
    let lines: [ConsoleTable.Line]

    var body: some View {
        Text(attributed)
            .font(ConsoleTheme.font)
            .lineSpacing(ConsoleTheme.lineSpacing)
            .multilineTextAlignment(.leading)
            .textSelection(.enabled)
            .fixedSize(horizontal: false, vertical: true)
            .frame(maxWidth: .infinity, alignment: .leading)
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
}
