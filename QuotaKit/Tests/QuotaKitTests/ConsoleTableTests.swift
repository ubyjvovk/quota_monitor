import Foundation
import Testing
@testable import QuotaKit

@Test func demoTableMatchesTheGoGoldenByteForByte() throws {
    let snapshot = try demoSnapshot()
    let golden = """
    Claude       max            live · just now
      Fable wk  █████░░░░░░░░░░░░░░░  23%  1d 16h
      Week      ███░░░░░░░░░░░░░░░░░  15%  1d 16h
      5h        █░░░░░░░░░░░░░░░░░░░   6%  2h 39m
      credits   20.00 (not enabled)

    ChatGPT      plus           live · just now
      5h        ████████████████████ 100%  8m
      Week      ██████░░░░░░░░░░░░░░  31%  5d 11h

    Grok         —              live · just now
      Week      █████████████░░░░░░░  63%  2d 13h

    DeepInfra    pay-as-you-go  live · just now
      balance   $10.03 remaining
      spend     $8.00 this month

    Kimi         basic          live · just now
      5h        ████████░░░░░░░░░░░░  42%  3h 12m
      Week      ███░░░░░░░░░░░░░░░░░  14%  4d 6h
    """

    #expect(ConsoleTable.text(snapshot.providers, asOf: snapshot.generatedAt) == golden)
}

@Test func spendDecodesFromTheCoreContract() throws {
    let snapshot = try demoSnapshot()
    let deepInfra = try #require(snapshot.providers.first { $0.id == "deepinfra" })
    let credits = try #require(deepInfra.credits)

    #expect(credits.spend == "$8.00 this month")
    #expect(credits.balance == "$10.03")
}

@Test func criticalRowColoursBarAndPercentOnly() throws {
    let snapshot = try demoSnapshot()
    let lines = ConsoleTable.render(snapshot.providers, asOf: snapshot.generatedAt)
    let criticalLine = try #require(lines.first { $0.text.contains("100%") })

    #expect(criticalLine.spans.map(\.text) == [
        "  5h        ",
        "████████████████████",
        " ",
        "100%",
        "  8m",
    ])
    #expect(criticalLine.spans.map(\.tone) == [
        .plain,
        .critical,
        .plain,
        .critical,
        .plain,
    ])

    for line in lines where !line.text.isEmpty && line != criticalLine {
        #expect(line.spans.count == 1)
        #expect(line.spans.first?.tone == .plain)
    }
}

@Test func warningToneAt70Percent() throws {
    let now = Date(timeIntervalSince1970: 1_000)
    let provider = ProviderSnapshot(
        id: "test",
        displayName: "Test",
        windows: [
            QuotaWindow(
                id: "warning",
                label: "At 70",
                kind: .session,
                usedPercent: 70,
                resetsAt: now.addingTimeInterval(3_600)
            ),
            QuotaWindow(
                id: "normal",
                label: "At 69",
                kind: .weekly,
                usedPercent: 69,
                resetsAt: now.addingTimeInterval(3_600)
            ),
        ],
        observedAt: now,
        origin: .live
    )
    let lines = ConsoleTable.render(provider, asOf: now)
    let warning = try #require(lines.first { $0.text.contains("At 70") })
    let normal = try #require(lines.first { $0.text.contains("At 69") })

    #expect(warning.spans.filter { $0.tone == .warning }.map(\.text) == [
        "██████████████",
        "70%",
    ])
    #expect(normal.spans.count == 1)
    #expect(normal.spans.first?.tone == .plain)
}

@Test func resetWindowRendersDashAndEmptyBar() throws {
    let now = Date(timeIntervalSince1970: 1_000)
    let provider = ProviderSnapshot(
        id: "test",
        displayName: "Test",
        windows: [
            QuotaWindow(
                id: "past",
                label: "Past",
                kind: .session,
                usedPercent: 88,
                resetsAt: now.addingTimeInterval(-1)
            ),
        ],
        observedAt: now,
        origin: .local
    )
    let line = try #require(ConsoleTable.render(provider, asOf: now).last)

    #expect(line.text == "  Past      " + String(repeating: "░", count: 20) + "    —  reset")
    #expect(line.spans.count == 1)
    #expect(line.spans.first?.tone == .plain)
}

@Test func statusLineFollowsTheBlock() throws {
    let now = Date(timeIntervalSince1970: 1_000)
    let provider = ProviderSnapshot(
        id: "codex",
        displayName: "ChatGPT",
        observedAt: now,
        origin: .unavailable,
        status: .needsSetup("run `codex login`")
    )

    let line = try #require(ConsoleTable.render(provider, asOf: now).last)
    #expect(line.text == "  !  run `codex login`")
    #expect(line.spans.count == 1)
    #expect(line.spans.first?.tone == .plain)
}

@Test func providersAreSeparatedByOneEmptyLine() {
    let now = Date(timeIntervalSince1970: 1_000)
    let first = ProviderSnapshot(
        id: "first",
        displayName: "First",
        observedAt: now,
        origin: .live
    )
    let second = ProviderSnapshot(
        id: "second",
        displayName: "Second",
        observedAt: now,
        origin: .local
    )

    let lines = ConsoleTable.render([first, second], asOf: now)
    #expect(lines.count == 3)
    #expect(lines[1].spans.isEmpty)
    #expect(lines.filter { $0.spans.isEmpty }.count == 1)
    #expect(lines.last?.text == "Second       —              cached · just now")
}

private func demoSnapshot() throws -> QuotaSnapshot {
    let fixtureURL = try #require(
        Bundle.module.url(
            forResource: "quotamon-demo",
            withExtension: "json",
            subdirectory: "Fixtures"
        )
    )
    return try QuotaSnapshot.decode(from: Data(contentsOf: fixtureURL))
}
