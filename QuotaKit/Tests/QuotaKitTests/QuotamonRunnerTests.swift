import Foundation
import Testing
@testable import QuotaKit

@Suite struct QuotamonRunnerTests {
    @Test func snapshotDecodesCoreFixtureAndFreshAddsItsArgument() async throws {
        let data = try fixtureData()
        let calls = CallRecorder()
        let runner = QuotamonRunner { received, standardInput in
            await calls.append(received, standardInput)
            return data
        }

        let snapshot = try await runner.snapshot()
        _ = try await runner.snapshot(fresh: true)

        #expect(snapshot.providers.map(\.id) == ["claude", "kimi"])
        #expect(await calls.arguments == [
            ["snapshot"],
            ["snapshot", "--fresh"],
        ])
        // A snapshot has no secret to pass, so nothing is written to the child.
        #expect(await calls.standardInputs == [nil, nil])
    }

    @Test func exitThreeBecomesAnActionableSetupError() async {
        let runner = QuotamonRunner { _, _ in
            throw QuotamonRunner.ProcessFailure(exitCode: 3, stderr: "not configured\n")
        }

        do {
            _ = try await runner.snapshot()
            Issue.record("Expected an exit-three failure")
        } catch QuotaError.notConfigured(let message) {
            #expect(message.contains("Run first-time setup"))
        } catch {
            Issue.record("Expected notConfigured, got \(error)")
        }
    }

    @Test func timeoutReportsTheDeadlineThatCoversTheCoresWorstCase() async {
        let runner = QuotamonRunner { _, _ in
            throw QuotamonRunner.ProcessFailure(exitCode: nil, stderr: "", timedOut: true)
        }

        do {
            _ = try await runner.snapshot()
            Issue.record("Expected a timeout failure")
        } catch QuotaError.transport(let message) {
            // 45 s, not 15: the core budgets a 25 s Kimi pre-fetch plus a 15 s
            // fetch, and the old cap killed runs that were about to succeed.
            #expect(QuotamonRunner.timeout == 45)
            #expect(message == "quotamon timed out after 45 seconds")
        } catch {
            Issue.record("Expected transport, got \(error)")
        }
    }

    @Test func invalidJSONBecomesAMalformedError() async {
        let runner = QuotamonRunner { _, _ in Data("not json".utf8) }

        do {
            _ = try await runner.snapshot()
            Issue.record("Expected malformed JSON to fail")
        } catch QuotaError.malformed {
            // Expected.
        } catch {
            Issue.record("Expected malformed, got \(error)")
        }
    }

    @Test func coreVersionAsksTheBinaryAndStripsItsBanner() async throws {
        let calls = CallRecorder()
        let runner = QuotamonRunner { received, standardInput in
            await calls.append(received, standardInput)
            return Data("quotamon 2026.9.1\n".utf8)
        }

        #expect(try await runner.coreVersion() == "2026.9.1")
        // The About panel asks for a version and has no secret to pass.
        #expect(await calls.arguments == [["--version"]])
        #expect(await calls.standardInputs == [nil])
    }

    @Test func coreVersionAcceptsAVersionPrintedWithoutTheBanner() async throws {
        let runner = QuotamonRunner { _, _ in Data("2026.9.1\n".utf8) }

        #expect(try await runner.coreVersion() == "2026.9.1")
    }

    @Test func coreVersionRejectsEmptyOutput() async {
        let runner = QuotamonRunner { _, _ in Data("  \n".utf8) }

        do {
            _ = try await runner.coreVersion()
            Issue.record("Expected empty output to fail")
        } catch QuotaError.malformed(let message) {
            #expect(message == "quotamon reported no version")
        } catch {
            Issue.record("Expected malformed, got \(error)")
        }
    }

    @Test func coreVersionReportsAFailedProcessAsAQuotaError() async {
        let runner = QuotamonRunner { _, _ in
            throw QuotamonRunner.ProcessFailure(exitCode: 1, stderr: "unknown flag: --version\n")
        }

        do {
            _ = try await runner.coreVersion()
            Issue.record("Expected a non-zero exit to fail")
        } catch QuotaError.transport(let message) {
            #expect(message == "unknown flag: --version")
        } catch {
            Issue.record("Expected transport, got \(error)")
        }
    }

    @Test func bundledRunnerFeedsStandardInputAndCapturesStandardOutput() async throws {
        // `cat` with no arguments is the cheapest possible echo of what the
        // spawn path writes to the child — it exercises the pipe, the argv and
        // the captured stdout without depending on the core being built.
        let runner = QuotamonRunner.bundled(executableURL: URL(fileURLWithPath: "/bin/cat"))

        let data = try await runner.run([], "a-secret-key")

        #expect(String(decoding: data, as: UTF8.self) == "a-secret-key\n")
    }

    @Test func bundledRunnerReportsANonZeroExitWithItsStandardError() async {
        let runner = QuotamonRunner.bundled(executableURL: URL(fileURLWithPath: "/bin/sh"))

        do {
            _ = try await runner.run(["-c", "echo boom >&2; exit 4"], nil)
            Issue.record("Expected a non-zero exit to fail")
        } catch QuotaError.transport(let message) {
            #expect(message == "boom")
        } catch {
            Issue.record("Expected transport, got \(error)")
        }
    }

    @Test func enginePersistsRunnerSnapshotAndKeepsItAfterFailure() async throws {
        let directory = temporaryDirectory()
        defer { try? FileManager.default.removeItem(at: directory) }
        let store = SnapshotStore(testDirectory: directory)
        let data = try fixtureData()
        let expected = try QuotaSnapshot.decode(from: data)
        let successfulRunner = QuotamonRunner { _, _ in data }
        let engine = await QuotaEngine(
            store: store,
            settings: QuotaSettings(),
            runner: successfulRunner
        )

        await engine.refresh()

        #expect(await engine.snapshot.providers == expected.providers)
        #expect(store.load() == expected)
        #expect(await engine.lastError == nil)

        let failingRunner = QuotamonRunner { _, _ in
            throw QuotaError.transport("stub refresh failed")
        }
        let failingEngine = await QuotaEngine(
            store: store,
            settings: QuotaSettings(),
            runner: failingRunner
        )

        await failingEngine.refresh()

        #expect(await failingEngine.snapshot == expected)
        #expect(await failingEngine.lastError == "stub refresh failed")
        #expect(store.load() == expected)
    }

    @Test func engineFreshRefreshPropagatesToRunner() async throws {
        let directory = temporaryDirectory()
        defer { try? FileManager.default.removeItem(at: directory) }
        let calls = CallRecorder()
        let data = try fixtureData()
        let runner = QuotamonRunner { received, standardInput in
            await calls.append(received, standardInput)
            return data
        }
        let engine = await QuotaEngine(
            store: SnapshotStore(testDirectory: directory),
            settings: QuotaSettings(),
            runner: runner
        )

        await engine.refresh(fresh: true)

        #expect(await calls.arguments == [["snapshot", "--fresh"]])
    }

    private func fixtureData() throws -> Data {
        let url = try #require(
            Bundle.module.url(
                forResource: "snapshot-v2",
                withExtension: "json",
                subdirectory: "Fixtures"
            )
        )
        return try Data(contentsOf: url)
    }

    private func temporaryDirectory() -> URL {
        FileManager.default.temporaryDirectory
            .appendingPathComponent("QuotamonRunnerTests-\(UUID().uuidString)", isDirectory: true)
    }
}

private actor CallRecorder {
    private(set) var arguments: [[String]] = []
    private(set) var standardInputs: [String?] = []

    func append(_ arguments: [String], _ standardInput: String?) {
        self.arguments.append(arguments)
        standardInputs.append(standardInput)
    }
}
