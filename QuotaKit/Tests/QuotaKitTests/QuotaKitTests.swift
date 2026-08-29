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

/// Lays out a temp directory shaped like `~/.codex` and returns its root.
private func makeCodexHome() throws -> URL {
    let home = URL(fileURLWithPath: NSTemporaryDirectory())
        .appendingPathComponent("codex-\(UUID().uuidString)")
    let day = home.appendingPathComponent("sessions/2026/07/31")
    try FileManager.default.createDirectory(at: day, withIntermediateDirectories: true)
    try FileManager.default.copyItem(
        at: try fixture("codex-rollout", "jsonl"),
        to: day.appendingPathComponent("rollout-2026-07-31T20-31-04-019fb9a8.jsonl")
    )
    return home
}

// MARK: - Codex local

@Test func codexLocalSourceReadsTheNewestRateLimitRecord() async throws {
    let home = try makeCodexHome()
    defer { try? FileManager.default.removeItem(at: home) }

    let snapshot = try await CodexLocalSource(home: home).fetch()

    #expect(snapshot.id == "codex")
    #expect(snapshot.displayName == "ChatGPT")
    #expect(snapshot.plan == "plus")
    #expect(snapshot.origin == .local)

    // The file holds two rate_limit records (5% then 18%); the later one wins.
    let primary = try #require(snapshot.windows.first { $0.id == "primary" })
    #expect(primary.usedPercent == 18.0)
    #expect(primary.windowMinutes == 10080)
    #expect(primary.kind == .weekly)
    #expect(primary.label == "Week")
    #expect(primary.resetsAt == primaryReset)

    let secondary = try #require(snapshot.windows.first { $0.id == "secondary" })
    #expect(secondary.usedPercent == 42.5)
    #expect(secondary.kind == .session)
    #expect(secondary.label == "5h")

    // observedAt must come from the record's own timestamp, not file mtime,
    // otherwise a copied file would look freshly observed.
    #expect(snapshot.observedAt == Date.fromISO8601("2026-07-31T19:31:13.804Z"))
}

@Test func codexSnapshotRanksTheMostConstrainedWindowFirst() async throws {
    let home = try makeCodexHome()
    defer { try? FileManager.default.removeItem(at: home) }

    let snapshot = try await CodexLocalSource(home: home).fetch()
    let tightest = try #require(snapshot.tightestWindow(asOf: beforeAll))
    #expect(tightest.id == "secondary")  // 42.5% beats 18%
}

@Test func tailReaderReturnsTheLastMatchingLine() throws {
    let url = try fixture("codex-rollout", "jsonl")
    let found = try CodexLocalSource.lastLine(containing: "rate_limits", in: url, tailBytes: 512 * 1024)
    let line = try #require(found)
    #expect(line.contains("\"used_percent\":18.0"))
    #expect(!line.contains("\"used_percent\":5.0"))
}

@Test func tailReaderEscalatesWhenTheWindowLandsMidRecord() throws {
    let url = try fixture("codex-rollout", "jsonl")
    // 700 bytes starts mid-record. The fragment must be discarded (never handed
    // to the parser) and the search must then widen rather than report nothing.
    let found = try CodexLocalSource.lastLine(containing: "rate_limits", in: url, tailBytes: 700)
    let line = try #require(found)

    let parsed = try JSONValue.parse(Data(line.utf8))  // a fragment would throw here
    #expect(parsed["timestamp"] != nil)
    #expect(line.contains("\"used_percent\":18.0"))
}

@Test func codexToleratesRecordsWithoutCredits() throws {
    // Older Codex builds omit `credits` entirely — must not fail the whole parse.
    let json = """
    {"limit_id":"codex","primary":{"used_percent":3.0,"window_minutes":10080,"resets_at":1785967367},\
    "secondary":null,"plan_type":"plus"}
    """
    let snapshot = try #require(
        Codex.snapshot(fromRateLimits: try JSONValue.parse(Data(json.utf8)),
                       observedAt: beforeAll, origin: .local)
    )
    #expect(snapshot.windows.count == 1)
    #expect(snapshot.credits == nil)
    #expect(snapshot.plan == "plus")
}

// MARK: - Claude local

@Test func claudeMirrorParsesBothWindows() async throws {
    let snapshot = try await ClaudeLocalSource(mirrorURL: try fixture("claude-mirror", "json")).fetch()

    #expect(snapshot.id == "claude")
    #expect(snapshot.plan == "max")
    #expect(snapshot.windows.count == 2)

    let fiveHour = try #require(snapshot.windows.first { $0.id == "five_hour" })
    #expect(fiveHour.usedPercent == 63.4)
    #expect(fiveHour.kind == .session)
    #expect(fiveHour.label == "5h")

    let weekly = try #require(snapshot.windows.first { $0.id == "seven_day" })
    #expect(weekly.usedPercent == 21.9)
    #expect(weekly.kind == .weekly)
}

@Test func claudeReportsSetupNeededWhenMirrorMissing() async {
    let missing = URL(fileURLWithPath: "/nonexistent/quota-monitor/claude-usage.json")
    await #expect(throws: QuotaError.self) {
        try await ClaudeLocalSource(mirrorURL: missing).fetch()
    }
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

// MARK: - Defensive JSON

@Test func jsonValueFindsNestedKeysAndCoercesTimestamps() throws {
    let json = """
    {"outer":{"inner":{"five_hour":{"utilization":51.5,"resets_at":"2026-07-31T19:31:13.804Z"}}}}
    """
    let root = try JSONValue.parse(Data(json.utf8))

    let node = try #require(root.firstValue(forKey: "five_hour"))
    #expect(node.firstValue(forAnyKey: ["used_percentage", "utilization"])?.double == 51.5)
    #expect(node["resets_at"]?.date == Date.fromISO8601("2026-07-31T19:31:13.804Z"))

    // Epoch seconds and milliseconds both resolve to the same instant.
    #expect(JSONValue.number(1_785_790_000).date == secondaryReset)
    #expect(JSONValue.number(1_785_790_000_000).date == secondaryReset)
    // Explicit nulls read as absent rather than as a value.
    #expect(try JSONValue.parse(Data(#"{"a":null}"#.utf8))["a"] == nil)
}

@Test func claudeParsingAcceptsAlternateKeySpellings() throws {
    // The statusLine payload says `used_percentage`; the usage endpoint may not.
    let json = #"{"rate_limits":{"five_hour":{"used_percent":12,"resetsAt":1785790000}}}"#
    let root = try JSONValue.parse(Data(json.utf8))
    let windows = Claude.windows(from: try #require(root.firstValue(forKey: "rate_limits")))

    #expect(windows.count == 1)
    #expect(windows[0].usedPercent == 12)
    #expect(windows[0].resetsAt == secondaryReset)
}

// MARK: - Menu bar title

@Test func menuBarTagsDistinguishClaudeFromChatGPT() {
    let claude = ProviderSnapshot(
        id: Claude.providerID, displayName: Claude.displayName,
        windows: [QuotaWindow(id: "five_hour", label: "5h", kind: .session,
                              usedPercent: 43, resetsAt: primaryReset)],
        observedAt: beforeAll, origin: .live
    )
    let codex = ProviderSnapshot(
        id: Codex.providerID, displayName: Codex.displayName,
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
        id: Claude.providerID, displayName: Claude.displayName, status: .needsSetup("x")
    )
    let title = QuotaFormat.menuBarTitle(for: QuotaSnapshot(providers: [dead]), asOf: beforeAll)
    #expect(title == "CL –")
    #expect(!title.contains("0%"))
}

// MARK: - Hybrid fallback

private struct StubSource: QuotaSource {
    let providerID = "stub"
    let displayName = "Stub"
    let origin: SnapshotOrigin
    let result: Result<ProviderSnapshot, QuotaError>

    func fetch() async throws -> ProviderSnapshot {
        try result.get()
    }
}

private func stubSnapshot(usedPercent: Double, origin: SnapshotOrigin) -> ProviderSnapshot {
    ProviderSnapshot(
        id: "stub", displayName: "Stub",
        windows: [QuotaWindow(id: "w", label: "Week", kind: .weekly, usedPercent: usedPercent)],
        observedAt: beforeAll, origin: origin
    )
}

@Test func liveReadingWinsWhenBothSucceed() async {
    let provider = HybridProvider(
        providerID: "stub", displayName: "Stub",
        local: StubSource(origin: .local, result: .success(stubSnapshot(usedPercent: 10, origin: .local))),
        live: StubSource(origin: .live, result: .success(stubSnapshot(usedPercent: 55, origin: .live)))
    )
    let result = await provider.fetch()
    #expect(result.origin == .live)
    #expect(result.windows[0].usedPercent == 55)
}

@Test func cachedReadingIsKeptAndLabelledWhenLiveFails() async {
    let provider = HybridProvider(
        providerID: "stub", displayName: "Stub",
        local: StubSource(origin: .local, result: .success(stubSnapshot(usedPercent: 10, origin: .local))),
        live: StubSource(origin: .live, result: .failure(.transport("HTTP 503")))
    )
    let result = await provider.fetch()

    // The number still shows — but never silently as if it were live.
    #expect(result.origin == .local)
    #expect(result.windows[0].usedPercent == 10)
    #expect(result.status.message?.contains("HTTP 503") == true)
}

@Test func disablingLiveSkipsTheEndpointEntirely() async {
    let provider = HybridProvider(
        providerID: "stub", displayName: "Stub",
        local: StubSource(origin: .local, result: .success(stubSnapshot(usedPercent: 10, origin: .local))),
        live: StubSource(origin: .live, result: .success(stubSnapshot(usedPercent: 99, origin: .live))),
        liveEnabled: false
    )
    let result = await provider.fetch()

    #expect(result.origin == .local)
    #expect(result.windows[0].usedPercent == 10)
    // No live attempt was made, so nothing to warn about.
    #expect(result.status.isOK)
}

// MARK: - Store

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
        id: Claude.providerID, displayName: Claude.displayName,
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
        id: Codex.providerID, displayName: Codex.displayName,
        windows: [window], observedAt: atElapsed(0.5), origin: .local
    )

    var history = UsageHistory(series: [
        UsageHistory.key(provider: Codex.providerID, window: "five_hour"): [
            // Belongs to the window before this one.
            UsageSample(at: paceReset.addingTimeInterval(-fiveHours - 3600), usedPercent: 95)
        ]
    ])
    history.record(QuotaSnapshot(providers: [provider]), at: atElapsed(0.5))

    let samples = history.samples(provider: Codex.providerID, window: "five_hour")
    // Carrying last window's 95% forward would draw a cliff at the reset.
    #expect(samples.count == 1)
    #expect(samples[0].usedPercent == 40)
}

@Test func historySkipsRepeatedIdenticalReadings() {
    let provider = ProviderSnapshot(
        id: Codex.providerID, displayName: Codex.displayName,
        windows: [paceWindow(usedPercent: 40)], observedAt: atElapsed(0.5), origin: .local
    )
    let snapshot = QuotaSnapshot(providers: [provider])

    var history = UsageHistory()
    history.record(snapshot, at: atElapsed(0.5))
    history.record(snapshot, at: atElapsed(0.51))  // local source unchanged between CLI turns

    #expect(history.samples(provider: Codex.providerID, window: "five_hour").count == 1)
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
        id: Codex.providerID, displayName: Codex.displayName, plan: "plus",
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
        id: Claude.providerID, displayName: Claude.displayName,
        windows: [paceWindow(usedPercent: 82), live],
        observedAt: atElapsed(0.5), origin: .local
    )

    // 4% that we actually know beats 82% that has since expired.
    #expect(provider.tightestWindow(asOf: afterReset)?.id == "seven_day")
    #expect(provider.sortedWindows(asOf: afterReset).first?.id == "seven_day")
}

// MARK: - Credential scoping

@Test func claudeTokenIsReadFromItsOwnSubtreeNotAnMCPServers() throws {
    // The real Keychain item stores Claude's OAuth beside `mcpOAuth`, a map of
    // per-MCP-server credentials that each carry their own `accessToken`.
    // A recursive key search returns an arbitrary one of these — in practice an
    // empty string — and the request then authenticates as nothing.
    let blob = """
    {"claudeAiOauth":{"accessToken":"sk-ant-oat01-REAL","expiresAt":4102444800000,
      "subscriptionType":"max","scopes":["user:inference"]},
     "mcpOAuth":{"plugin:sales:clay|abc":{"accessToken":""},
                 "plugin:marketing:canva|def":{"accessToken":"WRONG-TOKEN"}},
     "trustedDeviceToken":"nope"}
    """
    let credentials = try Claude.credentials(from: try JSONValue.parse(Data(blob.utf8)))

    #expect(credentials.token == "sk-ant-oat01-REAL")
    #expect(credentials.plan == "max")
    #expect(credentials.expiry != nil)
}

@Test func anEmptyClaudeTokenIsRejectedRatherThanSent() {
    let blob = #"{"claudeAiOauth":{"accessToken":""},"mcpOAuth":{"x":{"accessToken":"other"}}}"#
    #expect(throws: QuotaError.self) {
        _ = try Claude.credentials(from: try JSONValue.parse(Data(blob.utf8)))
    }
}

@Test func claudeCredentialsStillWorkWhenTheBlobIsAlreadyUnwrapped() throws {
    // Tolerates a bare OAuth object, in case the storage layout changes.
    let blob = #"{"accessToken":"sk-ant-oat01-BARE","subscriptionType":"pro"}"#
    let credentials = try Claude.credentials(from: try JSONValue.parse(Data(blob.utf8)))
    #expect(credentials.token == "sk-ant-oat01-BARE")
    #expect(credentials.plan == "pro")
}

// MARK: - Which failure gets reported

@Test func aBrokenConfiguredSourceOutranksAnUnconfiguredFallback() async {
    // The exact case that sent the user to reinstall a statusline when the real
    // problem was an expired token.
    let provider = HybridProvider(
        providerID: "stub", displayName: "Stub",
        local: StubSource(origin: .local,
                          result: .failure(.notConfigured("Statusline mirror not installed"))),
        live: StubSource(origin: .live,
                         result: .failure(.unauthorized("Claude sign-in expired")))
    )
    let result = await provider.fetch()

    #expect(result.origin == .unavailable)
    #expect(result.status.message == "Claude sign-in expired")
    #expect(result.status.message?.contains("Statusline") == false)
}

@Test func anUnconfiguredSourceIsStillReportedWhenItIsTheOnlyFailure() async {
    let provider = HybridProvider(
        providerID: "stub", displayName: "Stub",
        local: StubSource(origin: .local,
                          result: .failure(.notConfigured("Statusline mirror not installed"))),
        live: nil
    )
    let result = await provider.fetch()
    #expect(result.status.message == "Statusline mirror not installed")
}

@Test func staleWindowsReportTheirAgeRatherThanALiveRefreshError() async {
    // Local data whose window expired long ago, plus a failing live endpoint.
    // The age is the useful fact; the endpoint error is noise beside it.
    let expired = QuotaWindow(
        id: "primary", label: "Week", kind: .weekly, usedPercent: 18,
        resetsAt: Date().addingTimeInterval(-86_400 * 3), windowMinutes: 10080
    )
    let stale = ProviderSnapshot(
        id: "stub", displayName: "Stub", windows: [expired],
        observedAt: Date().addingTimeInterval(-86_400 * 15), origin: .local
    )

    let provider = HybridProvider(
        providerID: "stub", displayName: "Stub",
        local: StubSource(origin: .local, result: .success(stale)),
        live: StubSource(origin: .live, result: .failure(.transport("HTTP 404")))
    )
    let result = await provider.fetch()

    let message = result.status.message ?? ""
    #expect(message.contains("has since reset"))
    #expect(message.contains("15d ago"))
    #expect(!message.contains("404"))
}

@Test func aCurrentReadingStillSurfacesTheLiveRefreshError() async {
    // Only suppress the endpoint error when there is nothing current to caveat.
    let current = QuotaWindow(
        id: "primary", label: "Week", kind: .weekly, usedPercent: 18,
        resetsAt: Date().addingTimeInterval(86_400 * 2), windowMinutes: 10080
    )
    let fresh = ProviderSnapshot(
        id: "stub", displayName: "Stub", windows: [current],
        observedAt: Date().addingTimeInterval(-3600), origin: .local
    )

    let provider = HybridProvider(
        providerID: "stub", displayName: "Stub",
        local: StubSource(origin: .local, result: .success(fresh)),
        live: StubSource(origin: .live, result: .failure(.transport("HTTP 404")))
    )
    let result = await provider.fetch()
    #expect(result.status.message?.contains("404") == true)
}

// MARK: - Console report

private let reportNow = Date(timeIntervalSince1970: 2_000_000_000)
private let twoHoursThirtyNine = reportNow.addingTimeInterval(2 * 3600 + 39 * 60)
private let oneDayTwentyOneHours = reportNow.addingTimeInterval(86_400 + 21 * 3600)

@Test func consoleReportRendersBothWindowsWithPercentagesAndResets() {
    let snapshot = ProviderSnapshot(
        id: Claude.providerID, displayName: Claude.displayName, plan: "max",
        windows: [
            QuotaWindow(id: "five_hour", label: "5h", kind: .session,
                        usedPercent: 10, resetsAt: twoHoursThirtyNine, windowMinutes: 300),
            QuotaWindow(id: "seven_day", label: "Week", kind: .weekly,
                        usedPercent: 14, resetsAt: oneDayTwentyOneHours, windowMinutes: 10080),
        ],
        observedAt: reportNow.addingTimeInterval(-2), origin: .live
    )

    let text = ConsoleReport.render([snapshot], asOf: reportNow)

    #expect(text.contains("Claude"))
    #expect(text.contains("max"))
    #expect(text.contains("10.0%"))
    #expect(text.contains("14.0%"))
    #expect(text.contains("resets in 2h 39m"))
    #expect(text.contains("resets in 1d 21h"))
    #expect(text.contains("live"))
}

@Test func consoleReportRendersADashForAWindowWithNoReadingAndNeverPrintsZeroPercent() {
    let reset = QuotaWindow(
        id: "primary", label: "Week", kind: .weekly, usedPercent: 82,
        resetsAt: reportNow.addingTimeInterval(-3600), windowMinutes: 10080
    )
    let snapshot = ProviderSnapshot(
        id: Codex.providerID, displayName: Codex.displayName, plan: "plus",
        windows: [reset],
        observedAt: reportNow.addingTimeInterval(-15 * 86_400), origin: .local
    )

    let text = ConsoleReport.render([snapshot], asOf: reportNow)

    #expect(text.contains("—"))
    #expect(text.contains("no reading since this window reset"))
    #expect(!text.contains("0.0%"))
    #expect(!text.contains("82.0%"))
}

@Test func consoleReportRendersAnUnavailableProviderStatusMessage() {
    let snapshot = ProviderSnapshot.unavailable(
        id: Claude.providerID, displayName: Claude.displayName,
        status: .needsSetup("Not signed in — run `claude` to sign in"),
        observedAt: reportNow
    )

    let text = ConsoleReport.render([snapshot], asOf: reportNow)

    #expect(text.contains("unavailable"))
    #expect(text.contains("Not signed in — run `claude` to sign in"))
}

@Test func consoleReportTagsACachedProviderAndShowsItsAge() {
    let snapshot = ProviderSnapshot(
        id: Codex.providerID, displayName: Codex.displayName, plan: "plus",
        windows: [
            QuotaWindow(id: "primary", label: "Week", kind: .weekly,
                        usedPercent: 18, resetsAt: oneDayTwentyOneHours, windowMinutes: 10080),
        ],
        observedAt: reportNow.addingTimeInterval(-15 * 86_400), origin: .local
    )

    let text = ConsoleReport.render([snapshot], asOf: reportNow)

    #expect(text.contains("cached"))
    #expect(text.contains("15d ago"))
}

@Test func consoleReportJSONRoundTripsUsedPercentWithNullForAResetWindow() throws {
    let current = QuotaWindow(
        id: "five_hour", label: "5h", kind: .session, usedPercent: 10,
        resetsAt: twoHoursThirtyNine, windowMinutes: 300
    )
    let reset = QuotaWindow(
        id: "seven_day", label: "Week", kind: .weekly, usedPercent: 82,
        resetsAt: reportNow.addingTimeInterval(-3600), windowMinutes: 10080
    )
    let snapshot = ProviderSnapshot(
        id: Claude.providerID, displayName: Claude.displayName, plan: "max",
        windows: [current, reset],
        observedAt: reportNow.addingTimeInterval(-2), origin: .live
    )

    let json = try ConsoleReport.renderJSON([snapshot], asOf: reportNow)
    let root = try JSONSerialization.jsonObject(with: Data(json.utf8)) as? [String: Any]
    let provider = try #require(root?[Claude.providerID] as? [String: Any])
    let windows = try #require(provider["windows"] as? [[String: Any]])

    let percentsByID = Dictionary(uniqueKeysWithValues: windows.compactMap { row -> (String, Any)? in
        guard let id = row["id"] as? String, let percent = row["used_percent"] else { return nil }
        return (id, percent)
    })

    #expect((percentsByID["five_hour"] as? NSNumber)?.doubleValue == 10)
    #expect(percentsByID["seven_day"] is NSNull)
    #expect((percentsByID["seven_day"] as? NSNumber)?.doubleValue != 0)
}

@Test func providerCatalogListsClaudeThenCodexAndHonoursTheLiveSwitch() {
    let enabled = ProviderCatalog.all()
    #expect(enabled.map(\.providerID) == [Claude.providerID, Codex.providerID])
    #expect(enabled.allSatisfy { $0.liveEnabled })

    let disabled = ProviderCatalog.all(isLiveEnabled: { _ in false })
    #expect(disabled.map(\.providerID) == [Claude.providerID, Codex.providerID])
    #expect(disabled.allSatisfy { !$0.liveEnabled })
}
