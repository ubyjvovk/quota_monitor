import Foundation
import Testing
@testable import QuotaKit

// Fixture reset timestamps, ordered: beforeAll < secondaryReset < primaryReset
private let beforeAll = Date(timeIntervalSince1970: 1_785_700_000)
private let secondaryReset = Date(timeIntervalSince1970: 1_785_790_000)
private let primaryReset = Date(timeIntervalSince1970: 1_785_967_367)

private func fixture(_ name: String, _ ext: String) throws -> URL {
    try #require(Bundle.module.url(forResource: name, withExtension: ext, subdirectory: "Fixtures"))
}

// MARK: - Rollover

@Test func usageIsZeroedOnceTheWindowHasRolledOver() {
    let window = QuotaWindow(
        id: "seven_day", label: "Week", kind: .weekly,
        usedPercent: 82, resetsAt: secondaryReset, windowMinutes: 10080
    )

    // Before the reset the recorded usage stands.
    #expect(window.effectiveUsedPercent(asOf: beforeAll) == 82)
    #expect(window.hasRolledOver(asOf: beforeAll) == false)

    // After it, a stale local reading must not be shown as current usage.
    #expect(window.effectiveUsedPercent(asOf: primaryReset) == 0)
    #expect(window.effectiveRemainingPercent(asOf: primaryReset) == 100)
    #expect(window.hasRolledOver(asOf: primaryReset) == true)
    #expect(window.timeUntilReset(asOf: primaryReset) == nil)
}

@Test func windowLabelsDeriveFromDuration() {
    #expect(QuotaWindow.label(forMinutes: 300) == "5h")
    #expect(QuotaWindow.label(forMinutes: 10080) == "Week")
    #expect(QuotaWindow.label(forMinutes: 60 * 24 * 30) == "30d")
    #expect(QuotaWindow.label(forMinutes: nil) == "Usage")
    #expect(QuotaWindow.Kind.inferred(fromMinutes: 300) == .session)
    #expect(QuotaWindow.Kind.inferred(fromMinutes: 10080) == .weekly)
}

// MARK: - Aggregation

@Test func headlinePicksTheMostConstrainedWindowAcrossProviders() {
    let claude = ProviderSnapshot(
        id: "claude", displayName: "Claude",
        windows: [QuotaWindow(id: "five_hour", label: "5h", kind: .session,
                              usedPercent: 30, resetsAt: primaryReset)],
        observedAt: beforeAll, origin: .live
    )
    let codex = ProviderSnapshot(
        id: "codex", displayName: "ChatGPT",
        windows: [QuotaWindow(id: "primary", label: "Week", kind: .weekly,
                              usedPercent: 77, resetsAt: primaryReset)],
        observedAt: beforeAll, origin: .local
    )

    let headline = QuotaSnapshot(providers: [claude, codex]).headline(asOf: beforeAll)
    #expect(headline?.provider.id == "codex")
    #expect(headline?.window.usedPercent == 77)
}

@Test func unavailableProvidersRankBelowRealReadingsAndYieldNoHeadline() {
    let dead = ProviderSnapshot.unavailable(
        id: "claude", displayName: "Claude", status: .needsSetup("not installed")
    )
    #expect(QuotaSnapshot(providers: [dead]).headline(asOf: beforeAll) == nil)

    let live = ProviderSnapshot(
        id: "codex", displayName: "ChatGPT",
        windows: [QuotaWindow(id: "primary", label: "Week", kind: .weekly, usedPercent: 10)],
        observedAt: beforeAll, origin: .local
    )
    let ranked = QuotaSnapshot(providers: [dead, live]).rankedProviders(asOf: beforeAll)
    #expect(ranked.first?.id == "codex")
}

// MARK: - Menu bar title

@Test func menuBarTagsDistinguishClaudeFromChatGPT() {
    let claude = ProviderSnapshot(
        id: "claude", displayName: "Claude",
        windows: [QuotaWindow(id: "five_hour", label: "5h", kind: .session,
                              usedPercent: 43, resetsAt: primaryReset)],
        observedAt: beforeAll, origin: .live
    )
    let codex = ProviderSnapshot(
        id: "codex", displayName: "ChatGPT",
        windows: [QuotaWindow(id: "primary", label: "Week", kind: .weekly,
                              usedPercent: 18, resetsAt: primaryReset)],
        observedAt: beforeAll, origin: .local
    )

    // Both display names start with "C" — the tags must not collide.
    #expect(claude.shortName != codex.shortName)

    let title = QuotaFormat.menuBarTitle(for: QuotaSnapshot(providers: [claude, codex]), asOf: beforeAll)
    #expect(title == "CL 43%  ·  GPT 18%")
}

@Test func providersWithoutDataRenderADashNotZero() {
    let dead = ProviderSnapshot.unavailable(
        id: "claude", displayName: "Claude", status: .needsSetup("x")
    )
    let title = QuotaFormat.menuBarTitle(for: QuotaSnapshot(providers: [dead]), asOf: beforeAll)
    #expect(title == "CL –")
    #expect(!title.contains("0%"))
}

@Test func providerShortNamesMatchTheGoWaybarCatalog() {
    let names = [
        ("claude", "Claude", "CL"),
        ("codex", "ChatGPT", "GPT"),
        ("grok", "Grok", "GK"),
        ("deepinfra", "DeepInfra", "DI"),
        ("kimi", "Kimi", "KM"),
        ("runinfra", "RunInfra", "RI"),
        ("openrouter", "OpenRouter", "OR"),
        ("deepseek", "DeepSeek", "DS"),
    ]

    for (id, displayName, expected) in names {
        let provider = ProviderSnapshot(
            id: id, displayName: displayName,
            observedAt: beforeAll, origin: .live
        )
        #expect(provider.shortName == expected)
    }
}

// MARK: - Store

@Test func creditsWithoutEnabledDecodeAsEnabledForPersistedSnapshots() throws {
    let json = #"{"hasCredits":true,"unlimited":false,"balance":"12.50"}"#
    let credits = try JSONDecoder().decode(Credits.self, from: Data(json.utf8))

    #expect(credits.enabled == true)
}

@Test func snapshotSurvivesARoundTripThroughTheStore() throws {
    let directory = URL(fileURLWithPath: NSTemporaryDirectory())
        .appendingPathComponent("quota-store-\(UUID().uuidString)")
    defer { try? FileManager.default.removeItem(at: directory) }

    // No App Group in tests, so the store falls back to a plain directory.
    let store = SnapshotStore(appGroupID: nil)
    #expect(store.isSharedWithWidget == false)

    let original = QuotaSnapshot(
        providers: [ProviderSnapshot(
            id: "codex", displayName: "ChatGPT", plan: "plus",
            windows: [QuotaWindow(id: "primary", label: "Week", kind: .weekly,
                                  usedPercent: 18, resetsAt: primaryReset, windowMinutes: 10080)],
            credits: Credits(hasCredits: false, unlimited: false, balance: "0"),
            observedAt: beforeAll, origin: .local
        )],
        generatedAt: beforeAll
    )

    let restored = try QuotaSnapshot.decode(from: try original.encoded())
    #expect(restored == original)
    #expect(restored.providers[0].windows[0].resetsAt == primaryReset)
}

@Test func disabledCreditsSurviveAPersistedSnapshotRoundTrip() throws {
    let provider = ProviderSnapshot(
        id: "claude", displayName: "Claude", plan: "max",
        credits: Credits(
            hasCredits: false, unlimited: false, balance: "20.00", enabled: false
        ),
        observedAt: beforeAll, origin: .live
    )
    let original = QuotaSnapshot(providers: [provider], generatedAt: beforeAll)

    let restored = try QuotaSnapshot.decode(from: original.encoded())

    #expect(restored.providers.first?.credits == provider.credits)
}

@Test func snapshotV2FixtureDecodesInQuotaKit() throws {
    let data = try Data(contentsOf: fixture("snapshot-v2", "json"))
    let snapshot = try QuotaSnapshot.decode(from: data)

    #expect(snapshot.providers.count == 2)

    let claude = try #require(snapshot.providers.first { $0.id == "claude" })
    #expect(claude.windows.count == 3)
    #expect(claude.credits?.enabled == false)
    #expect(claude.status == .ok)

    let kimi = try #require(snapshot.providers.first { $0.id == "kimi" })
    #expect(kimi.status == .needsSetup("Run `kimi` and capture a fresh usage reading"))
}

@Test func providerStatusDecodesLegacyShapesAndEncodesV2() throws {
    let decoder = JSONDecoder()
    let setup = try decoder.decode(
        ProviderStatus.self,
        from: Data(#"{"needsSetup":{"_0":"x"}}"#.utf8)
    )
    let ok = try decoder.decode(ProviderStatus.self, from: Data(#"{"ok":{}}"#.utf8))

    #expect(setup == .needsSetup("x"))
    #expect(ok == .ok)

    let encoder = JSONEncoder()
    encoder.outputFormatting = [.sortedKeys]
    let encoded = try encoder.encode(ProviderStatus.needsSetup("x"))
    #expect(String(decoding: encoded, as: UTF8.self) == #"{"message":"x","state":"needsSetup"}"#)
}

// MARK: - Pace

private let paceReset = Date(timeIntervalSince1970: 1_800_000_000)
private let fiveHours: TimeInterval = 5 * 3600

private func paceWindow(usedPercent: Double) -> QuotaWindow {
    QuotaWindow(id: "five_hour", label: "5h", kind: .session,
                usedPercent: usedPercent, resetsAt: paceReset, windowMinutes: 300)
}

/// `fraction` of the way through the window.
private func atElapsed(_ fraction: Double) -> Date {
    paceReset.addingTimeInterval(-fiveHours + fiveHours * fraction)
}

@Test func spendingExactlyEvenlyIsOnPaceAndNeverProjectsExhaustion() throws {
    let pace = try #require(paceWindow(usedPercent: 50).pace(asOf: atElapsed(0.5)))

    #expect(abs(pace.ratio - 1.0) < 0.001)
    #expect(pace.verdict == .onPace)
    // Exactly on pace lands on the reset, not before it — so no warning.
    #expect(pace.projectedExhaustion == nil)
}

@Test func burningTwiceAsFastProjectsExhaustionAtTheWindowMidpoint() throws {
    // 60% consumed with only 30% of the window gone.
    let pace = try #require(paceWindow(usedPercent: 60).pace(asOf: atElapsed(0.3)))

    #expect(abs(pace.ratio - 2.0) < 0.001)
    #expect(pace.verdict == .overspending)

    let exhaustion = try #require(pace.projectedExhaustion)
    #expect(exhaustion < paceReset)
    // At 2x rate the allowance is gone halfway through.
    #expect(abs(exhaustion.timeIntervalSince(atElapsed(0.5))) < 1)
}

@Test func lightUseReadsAsComfortable() throws {
    let pace = try #require(paceWindow(usedPercent: 5).pace(asOf: atElapsed(0.5)))
    #expect(pace.verdict == .comfortable)
    #expect(pace.projectedExhaustion == nil)
}

@Test func aFreshWindowRefusesToJudgeTheRate() throws {
    // One request a minute into a five-hour window is not a 60x burn rate.
    let pace = try #require(paceWindow(usedPercent: 3).pace(asOf: atElapsed(0.005)))
    #expect(pace.verdict == .tooEarly)
    #expect(pace.projectedExhaustion == nil)
    #expect(QuotaFormat.paceSummary(for: paceWindow(usedPercent: 3), asOf: atElapsed(0.005))
        == "window just reset")
}

@Test func paceIsUnavailableWithoutWindowBounds() {
    // No windowMinutes means no start, so there is no rate to compute.
    let unbounded = QuotaWindow(id: "x", label: "x", kind: .other,
                                usedPercent: 50, resetsAt: paceReset)
    #expect(unbounded.pace(asOf: atElapsed(0.5)) == nil)
}

@Test func summaryNamesTheProviderAndTimeWhenSomethingWillRunOut() {
    let provider = ProviderSnapshot(
        id: "claude", displayName: "Claude",
        windows: [paceWindow(usedPercent: 60)],
        observedAt: atElapsed(0.3), origin: .live
    )
    let finding = QuotaFormat.finding(for: QuotaSnapshot(providers: [provider]), asOf: atElapsed(0.3))
    #expect(finding.contains("Claude"))
    #expect(finding.contains("runs out in"))
}

// MARK: - History

@Test func historyDropsSamplesFromAPreviousWindow() {
    let window = paceWindow(usedPercent: 40)
    let provider = ProviderSnapshot(
        id: "codex", displayName: "ChatGPT",
        windows: [window], observedAt: atElapsed(0.5), origin: .local
    )

    var history = UsageHistory(series: [
        UsageHistory.key(provider: "codex", window: "five_hour"): [
            // Belongs to the window before this one.
            UsageSample(at: paceReset.addingTimeInterval(-fiveHours - 3600), usedPercent: 95)
        ]
    ])
    history.record(QuotaSnapshot(providers: [provider]), at: atElapsed(0.5))

    let samples = history.samples(provider: "codex", window: "five_hour")
    // Carrying last window's 95% forward would draw a cliff at the reset.
    #expect(samples.count == 1)
    #expect(samples[0].usedPercent == 40)
}

@Test func historySkipsRepeatedIdenticalReadings() {
    let provider = ProviderSnapshot(
        id: "codex", displayName: "ChatGPT",
        windows: [paceWindow(usedPercent: 40)], observedAt: atElapsed(0.5), origin: .local
    )
    let snapshot = QuotaSnapshot(providers: [provider])

    var history = UsageHistory()
    history.record(snapshot, at: atElapsed(0.5))
    history.record(snapshot, at: atElapsed(0.51))  // local source unchanged between CLI turns

    #expect(history.samples(provider: "codex", window: "five_hour").count == 1)
}

@Test func historySkipsARolledOverWindowRatherThanRecordingZero() {
    let window = paceWindow(usedPercent: 82)
    let provider = ProviderSnapshot(
        id: "codex", displayName: "ChatGPT",
        windows: [window], observedAt: atElapsed(0.5), origin: .local
    )
    let key = UsageHistory.key(provider: "codex", window: "five_hour")
    let original = UsageSample(at: atElapsed(0.5), usedPercent: 82)
    var history = UsageHistory(series: [key: [original]])

    history.record(
        QuotaSnapshot(providers: [provider]),
        at: paceReset.addingTimeInterval(3600)
    )

    #expect(history.samples(provider: "codex", window: "five_hour") == [original])
}

// MARK: - Unknown vs zero

@Test func aResetWindowReportsNoReadingRatherThanZeroUsage() {
    let window = paceWindow(usedPercent: 82)
    let afterReset = paceReset.addingTimeInterval(3600)

    // Before the reset the reading stands.
    #expect(window.currentUsedPercent(asOf: atElapsed(0.5)) == 82)

    // After it we know the old number is void — but not that usage is zero.
    #expect(window.currentUsedPercent(asOf: afterReset) == nil)
    #expect(QuotaFormat.percentOrDash(window.currentUsedPercent(asOf: afterReset)) == "—")
    #expect(QuotaFormat.paceSummary(for: window, asOf: afterReset)
        == "no reading since this window reset")
}

@Test func staleProvidersProduceNoHeadlineAndNoFabricatedPercentage() {
    let afterReset = paceReset.addingTimeInterval(3600)
    let stale = ProviderSnapshot(
        id: "codex", displayName: "ChatGPT", plan: "plus",
        windows: [paceWindow(usedPercent: 82)],
        observedAt: atElapsed(0.5), origin: .local
    )
    let snapshot = QuotaSnapshot(providers: [stale])

    #expect(snapshot.headline(asOf: afterReset) == nil)
    #expect(QuotaFormat.finding(for: snapshot, asOf: afterReset) == "No current usage data")
    // The menu bar must not claim 0%.
    #expect(QuotaFormat.menuBarTitle(for: snapshot, asOf: afterReset) == "GPT –")
}

@Test func windowsWithRealReadingsOutrankStaleOnes() {
    let afterReset = paceReset.addingTimeInterval(3600)
    let live = QuotaWindow(id: "seven_day", label: "Week", kind: .weekly,
                           usedPercent: 4,
                           resetsAt: afterReset.addingTimeInterval(86_400),
                           windowMinutes: 10080)
    let provider = ProviderSnapshot(
        id: "claude", displayName: "Claude",
        windows: [paceWindow(usedPercent: 82), live],
        observedAt: atElapsed(0.5), origin: .local
    )

    // 4% that we actually know beats 82% that has since expired.
    #expect(provider.tightestWindow(asOf: afterReset)?.id == "seven_day")
    #expect(provider.sortedWindows(asOf: afterReset).first?.id == "seven_day")
}
