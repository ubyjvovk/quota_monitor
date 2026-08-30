import Foundation
import Testing
@testable import QuotaKit

@Suite struct QuotamonRunnerTests {
    @Test func snapshotDecodesCoreFixtureAndFreshAddsItsArgument() async throws {
        let data = try fixtureData()
        let arguments = ArgumentRecorder()
        let runner = QuotamonRunner { received in
            await arguments.append(received)
            return data
        }

        let snapshot = try await runner.snapshot()
        _ = try await runner.snapshot(fresh: true)

        #expect(snapshot.providers.map(\.id) == ["claude", "kimi"])
        #expect(await arguments.values == [
            ["snapshot", "--json"],
            ["snapshot", "--json", "--fresh"],
        ])
    }

    @Test func exitThreeBecomesAnActionableSetupError() async {
        let runner = QuotamonRunner { _ in
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

    @Test func invalidJSONBecomesAMalformedError() async {
        let runner = QuotamonRunner { _ in Data("not json".utf8) }

        do {
            _ = try await runner.snapshot()
            Issue.record("Expected malformed JSON to fail")
        } catch QuotaError.malformed {
            // Expected.
        } catch {
            Issue.record("Expected malformed, got \(error)")
        }
    }

    @Test func enginePersistsRunnerSnapshotAndKeepsItAfterFailure() async throws {
        let directory = temporaryDirectory()
        defer { try? FileManager.default.removeItem(at: directory) }
        let store = SnapshotStore(testDirectory: directory)
        let data = try fixtureData()
        let expected = try QuotaSnapshot.decode(from: data)
        let successfulRunner = QuotamonRunner { _ in data }
        let engine = await QuotaEngine(
            store: store,
            settings: QuotaSettings(),
            runner: successfulRunner
        )

        await engine.refresh()

        #expect(await engine.snapshot.providers == expected.providers)
        #expect(store.load() == expected)
        #expect(await engine.lastError == nil)

        let failingRunner = QuotamonRunner { _ in
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
        let arguments = ArgumentRecorder()
        let data = try fixtureData()
        let runner = QuotamonRunner { received in
            await arguments.append(received)
            return data
        }
        let engine = await QuotaEngine(
            store: SnapshotStore(testDirectory: directory),
            settings: QuotaSettings(),
            runner: runner
        )

        await engine.refresh(fresh: true)

        #expect(await arguments.values == [["snapshot", "--json", "--fresh"]])
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

private actor ArgumentRecorder {
    private(set) var values: [[String]] = []

    func append(_ arguments: [String]) {
        values.append(arguments)
    }
}
