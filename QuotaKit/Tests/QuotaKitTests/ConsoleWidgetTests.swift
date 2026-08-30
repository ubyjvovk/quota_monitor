#if canImport(SwiftUI) && canImport(AppKit)
import Foundation
import Testing
@testable import QuotaKit

@Test func smallRowsAreSeventeenColumnsTightestFirst() throws {
    let snapshot = try demoWidgetSnapshot()
    let now = snapshot.generatedAt
    let lines = ConsoleWidgetView.lines(for: snapshot, asOf: now, size: .small)

    #expect(!lines.isEmpty)
    for line in lines {
        #expect(line.text.unicodeScalars.count == 17)
    }

    let first = try #require(lines.first)
    #expect(first.text == "GPT ████████ 100%")
    #expect(first.spans.map(\.text) == ["GPT ", "████████", " ", "100%"])
    #expect(first.spans.map(\.tone) == [.plain, .critical, .plain, .critical])
}

@Test func mediumRowsReuseTheConsoleBarAndCountdown() throws {
    let snapshot = try demoWidgetSnapshot()
    let now = snapshot.generatedAt
    let lines = ConsoleWidgetView.lines(for: snapshot, asOf: now, size: .medium)

    let chatGPT = try #require(lines.first)
    #expect(chatGPT.text == "GPT ████████████████████ 100%  8m")

    // Grok sits at 63%, which is the console's own `round(63 / 5)` = 13 cells.
    let grok = try #require(lines.first { $0.text.contains("63%") })
    #expect(grok.text.filter { $0 == "█" }.count == 13)
}

@Test func largeIsTheConsoleTable() throws {
    let snapshot = try demoWidgetSnapshot()
    let now = snapshot.generatedAt

    #expect(
        ConsoleWidgetView.lines(for: snapshot, asOf: now, size: .large)
            == ConsoleTable.render(snapshot.providers, asOf: now)
    )
}

@Test func rolledOverWindowsAreDropped() {
    let now = Date(timeIntervalSince1970: 100_000)
    let snapshot = QuotaSnapshot(
        providers: [
            ProviderSnapshot(
                id: "stale", displayName: "Stale",
                windows: [
                    QuotaWindow(id: "past", label: "5h", kind: .session,
                                usedPercent: 88, resetsAt: now.addingTimeInterval(-1)),
                ],
                observedAt: now, origin: .local
            ),
            ProviderSnapshot(
                id: "fresh", displayName: "Fresh",
                windows: [
                    QuotaWindow(id: "live", label: "5h", kind: .session,
                                usedPercent: 50, resetsAt: now.addingTimeInterval(3_600)),
                ],
                observedAt: now, origin: .live
            ),
        ],
        generatedAt: now
    )

    for size in [ConsoleWidgetView.Size.small, .medium] {
        let lines = ConsoleWidgetView.lines(for: snapshot, asOf: now, size: size)
        #expect(lines.count == 1)
        #expect(lines.first?.text.hasPrefix("FR") == true)
    }
}

@Test func emptySnapshotHasNoLines() {
    let now = Date(timeIntervalSince1970: 100_000)
    for size in [ConsoleWidgetView.Size.small, .medium, .large] {
        #expect(ConsoleWidgetView.lines(for: .empty, asOf: now, size: size).isEmpty)
    }
}

private func demoWidgetSnapshot() throws -> QuotaSnapshot {
    let fixtureURL = try #require(
        Bundle.module.url(
            forResource: "quotamon-demo",
            withExtension: "json",
            subdirectory: "Fixtures"
        )
    )
    return try QuotaSnapshot.decode(from: Data(contentsOf: fixtureURL))
}
#endif
