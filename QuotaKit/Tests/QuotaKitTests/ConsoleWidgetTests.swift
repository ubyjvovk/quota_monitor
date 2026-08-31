#if canImport(SwiftUI) && canImport(AppKit)
import Foundation
import Testing
@testable import QuotaKit

@Test func smallRowsAreSeventeenColumnsTightestFirst() throws {
    let snapshot = try demoWidgetSnapshot()
    let now = snapshot.generatedAt
    let lines = ConsoleWidgetView.lines(for: snapshot, asOf: now, size: .small)

    #expect(!lines.isEmpty)
    // Bar rows hold the 17-cell budget; the credits rows that follow them are
    // text, not bars, and have their own test.
    for line in lines where line.text.contains("░") || line.text.contains("█") {
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

@Test func creditsOnlyProvidersGetARowAfterTheBarRows() throws {
    let snapshot = try demoWidgetSnapshot()
    let now = snapshot.generatedAt

    for size in [ConsoleWidgetView.Size.small, .medium] {
        let lines = ConsoleWidgetView.lines(for: snapshot, asOf: now, size: size)
        // DeepInfra reports no window at all — only a spendable balance.
        #expect(lines.last?.text == "DI  $10.03 remaining")
        #expect(lines.dropLast().allSatisfy { $0.text.contains("░") || $0.text.contains("█") })
    }
}

@Test func aWindowlessProviderWithNothingSpendableGetsNoRow() {
    let now = Date(timeIntervalSince1970: 100_000)
    let snapshot = QuotaSnapshot(
        providers: [
            ProviderSnapshot(
                id: "flat", displayName: "Flat",
                credits: Credits(hasCredits: true, unlimited: false, balance: "$0.00", enabled: false),
                observedAt: now, origin: .live
            ),
        ],
        generatedAt: now
    )

    for size in [ConsoleWidgetView.Size.small, .medium] {
        #expect(ConsoleWidgetView.lines(for: snapshot, asOf: now, size: size).isEmpty)
    }
}

@Test func cachedOrErroredProvidersCarryTheStalenessMarker() {
    let now = Date(timeIntervalSince1970: 100_000)
    func window(_ used: Double) -> QuotaWindow {
        QuotaWindow(id: "w", label: "5h", kind: .session,
                    usedPercent: used, resetsAt: now.addingTimeInterval(3_600))
    }
    let snapshot = QuotaSnapshot(
        providers: [
            ProviderSnapshot(id: "cached", displayName: "Cached", windows: [window(50)],
                             observedAt: now, origin: .local),
            ProviderSnapshot(id: "errored", displayName: "Errored", windows: [window(60)],
                             observedAt: now, origin: .live,
                             status: .failed("live refresh failed")),
            ProviderSnapshot(id: "fresh", displayName: "Fresh", windows: [window(70)],
                             observedAt: now, origin: .live),
            ProviderSnapshot(id: "money", displayName: "Money",
                             credits: Credits(hasCredits: true, unlimited: false,
                                              balance: "$4.00", enabled: true),
                             observedAt: now, origin: .local),
        ],
        generatedAt: now
    )

    for size in [ConsoleWidgetView.Size.small, .medium] {
        let lines = ConsoleWidgetView.lines(for: snapshot, asOf: now, size: size)
        // Tightest first, then the credits-only row: fresh, errored, cached, money.
        #expect(lines.map { $0.text.hasSuffix(" *") } == [false, true, true, true])
        #expect(lines.last?.text == "MO  $4.00 remaining *")
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
