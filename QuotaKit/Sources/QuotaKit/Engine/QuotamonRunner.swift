import Darwin
import Foundation

/// Runs the bundled `quotamon` executable and decodes its snapshot output.
public struct QuotamonRunner: Sendable {
    /// Runs `quotamon` with the supplied arguments and returns its standard output.
    ///
    /// Inject this closure in tests so they never spawn a process.
    public let run: @Sendable ([String]) async throws -> Data

    /// Creates a runner around an asynchronous execution function.
    public init(run: @escaping @Sendable ([String]) async throws -> Data) {
        self.run = run
    }

    /// Creates a runner that executes the binary at `executableURL` with a
    /// 15-second wall-clock limit.
    public static func bundled(executableURL: URL) -> QuotamonRunner {
        QuotamonRunner { arguments in
            do {
                return try await Task.detached {
                    try execute(executableURL: executableURL, arguments: arguments)
                }.value
            } catch let failure as ProcessFailure {
                throw quotaError(for: failure)
            } catch let error as QuotaError {
                throw error
            } catch {
                throw QuotaError.transport("Could not run quotamon — \(error.localizedDescription)")
            }
        }
    }

    /// Fetches and decodes a snapshot, bypassing core caches when `fresh` is true.
    public func snapshot(fresh: Bool = false) async throws -> QuotaSnapshot {
        var arguments = ["snapshot", "--json"]
        if fresh { arguments.append("--fresh") }

        let data: Data
        do {
            data = try await run(arguments)
        } catch let failure as ProcessFailure {
            throw Self.quotaError(for: failure)
        } catch let error as QuotaError {
            throw error
        } catch {
            throw QuotaError.transport("Could not run quotamon — \(error.localizedDescription)")
        }

        do {
            return try QuotaSnapshot.decode(from: data)
        } catch {
            throw QuotaError.malformed("quotamon returned malformed snapshot JSON")
        }
    }

    struct ProcessFailure: Error, Sendable {
        let exitCode: Int32?
        let stderr: String
        let timedOut: Bool

        init(exitCode: Int32?, stderr: String, timedOut: Bool = false) {
            self.exitCode = exitCode
            self.stderr = stderr.trimmingCharacters(in: .whitespacesAndNewlines)
            self.timedOut = timedOut
        }
    }

    private static func quotaError(for failure: ProcessFailure) -> QuotaError {
        if failure.exitCode == 3 {
            return .notConfigured("Run first-time setup for quotamon")
        }
        if failure.timedOut {
            return .transport("quotamon timed out after 15 seconds")
        }

        let message = failure.stderr.isEmpty
            ? "quotamon exited with status \(failure.exitCode ?? -1)"
            : failure.stderr
        return .transport(message)
    }

    private static func execute(executableURL: URL, arguments: [String]) throws -> Data {
        let fileManager = FileManager.default
        let directory = fileManager.temporaryDirectory
            .appendingPathComponent("QuotamonRunner-\(UUID().uuidString)", isDirectory: true)
        try fileManager.createDirectory(at: directory, withIntermediateDirectories: true)
        defer { try? fileManager.removeItem(at: directory) }

        let stdoutURL = directory.appendingPathComponent("stdout")
        let stderrURL = directory.appendingPathComponent("stderr")
        guard fileManager.createFile(atPath: stdoutURL.path, contents: nil),
              fileManager.createFile(atPath: stderrURL.path, contents: nil)
        else {
            throw QuotaError.transport("Could not prepare output capture for quotamon")
        }

        let stdout = try FileHandle(forWritingTo: stdoutURL)
        let stderr = try FileHandle(forWritingTo: stderrURL)
        defer {
            try? stdout.close()
            try? stderr.close()
        }

        let process = Process()
        process.executableURL = executableURL
        process.arguments = arguments
        process.standardOutput = stdout
        process.standardError = stderr
        try process.run()

        let deadline = Date().addingTimeInterval(15)
        while process.isRunning, Date() < deadline {
            Thread.sleep(forTimeInterval: 0.02)
        }

        if process.isRunning {
            process.terminate()
            let graceDeadline = Date().addingTimeInterval(0.25)
            while process.isRunning, Date() < graceDeadline {
                Thread.sleep(forTimeInterval: 0.01)
            }
            if process.isRunning {
                kill(process.processIdentifier, SIGKILL)
                process.waitUntilExit()
            }
            throw ProcessFailure(exitCode: nil, stderr: "", timedOut: true)
        }

        try stdout.synchronize()
        try stderr.synchronize()
        let output = try Data(contentsOf: stdoutURL)
        guard process.terminationStatus == 0 else {
            let errorData = try Data(contentsOf: stderrURL)
            let errorText = String(decoding: errorData, as: UTF8.self)
            throw ProcessFailure(exitCode: process.terminationStatus, stderr: errorText)
        }
        return output
    }
}
