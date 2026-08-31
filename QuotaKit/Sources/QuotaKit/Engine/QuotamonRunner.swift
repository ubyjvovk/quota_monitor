import Darwin
import Foundation

/// Runs the bundled `quotamon` executable and decodes its snapshot output.
public struct QuotamonRunner: Sendable {
    /// Runs `quotamon` with the supplied arguments, optionally feeding the child
    /// process one line on standard input, and returns its standard output.
    ///
    /// The `standardInput` half exists so a secret — an API key for
    /// `config set` — reaches the core without ever appearing in argv, which any
    /// user on the machine can read out of `ps`.
    ///
    /// Inject this closure in tests so they never spawn a process.
    public let run: @Sendable ([String], String?) async throws -> Data

    /// Creates a runner around an asynchronous execution function.
    public init(run: @escaping @Sendable ([String], String?) async throws -> Data) {
        self.run = run
    }

    /// How long the app waits for `quotamon` before killing it.
    ///
    /// The core's own worst case is two budgets in series: 25 s for the Kimi
    /// token pre-fetch (it launches the vendor CLI on its own 20 s clock) and a
    /// further 15 s for the provider fetch. 45 s covers both plus margin. The
    /// previous 15 s cap could kill a run the core was about to finish and
    /// report a timeout instead of the snapshot it was seconds away from
    /// printing.
    public static let timeout: TimeInterval = 45

    /// Creates a runner that executes the binary at `executableURL` under the
    /// wall-clock limit described on ``timeout``.
    public static func bundled(executableURL: URL) -> QuotamonRunner {
        QuotamonRunner { arguments, standardInput in
            do {
                return try await Task.detached {
                    try execute(
                        executableURL: executableURL,
                        arguments: arguments,
                        standardInput: standardInput
                    )
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
        // `snapshot` already emits JSON; the subcommand rejects a `--json` flag
        // (that spelling is only the bare-table alias `quotamon --json`).
        var arguments = ["snapshot"]
        if fresh { arguments.append("--fresh") }

        let data: Data
        do {
            data = try await run(arguments, nil)
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
            return .transport("quotamon timed out after \(Int(timeout)) seconds")
        }

        let message = failure.stderr.isEmpty
            ? "quotamon exited with status \(failure.exitCode ?? -1)"
            : failure.stderr
        return .transport(message)
    }

    private static func execute(
        executableURL: URL,
        arguments: [String],
        standardInput: String?
    ) throws -> Data {
        let fileManager = FileManager.default
        let directory = fileManager.temporaryDirectory
            .appendingPathComponent("QuotamonRunner-\(UUID().uuidString)", isDirectory: true)
        try fileManager.createDirectory(at: directory, withIntermediateDirectories: true)
        defer { try? fileManager.removeItem(at: directory) }

        let stdoutURL = directory.appendingPathComponent("stdout")
        let stderrURL = directory.appendingPathComponent("stderr")

        let inputFD = try standardInput.map(filledPipe(with:))
        defer { if let inputFD { close(inputFD) } }

        let pid = try spawn(
            executableURL: executableURL,
            arguments: arguments,
            stdoutPath: stdoutURL.path,
            stderrPath: stderrURL.path,
            inputFD: inputFD
        )

        guard let status = wait(for: pid, until: Date().addingTimeInterval(timeout)) else {
            terminateGroup(pid)
            throw ProcessFailure(exitCode: nil, stderr: "", timedOut: true)
        }

        guard exitCode(from: status) == 0 else {
            let errorData = (try? Data(contentsOf: stderrURL)) ?? Data()
            throw ProcessFailure(
                exitCode: exitCode(from: status),
                stderr: String(decoding: errorData, as: UTF8.self)
            )
        }
        return try Data(contentsOf: stdoutURL)
    }

    /// A pipe read end already holding `text` and one newline, with the write
    /// end closed.
    ///
    /// Pre-filling before the child exists is what makes this safe: the parent
    /// never races the reader and can never take a SIGPIPE. It works only
    /// because the one thing sent this way is a short secret that fits the pipe
    /// buffer whole; anything larger is refused rather than risking a deadlock.
    private static func filledPipe(with text: String) throws -> Int32 {
        let bytes = Array((text + "\n").utf8)
        guard bytes.count <= 4096 else {
            throw QuotaError.transport("Could not pass input to quotamon — the value is too large")
        }

        var descriptors: [Int32] = [-1, -1]
        guard pipe(&descriptors) == 0 else {
            throw QuotaError.transport("Could not prepare input for quotamon")
        }
        let readFD = descriptors[0]
        let writeFD = descriptors[1]

        var offset = 0
        while offset < bytes.count {
            let written = bytes[offset...].withUnsafeBufferPointer {
                write(writeFD, $0.baseAddress, $0.count)
            }
            if written < 0, errno == EINTR { continue }
            guard written > 0 else {
                close(readFD)
                close(writeFD)
                throw QuotaError.transport("Could not prepare input for quotamon")
            }
            offset += written
        }
        close(writeFD)
        return readFD
    }

    /// Spawns the child in a process group of its own and returns its pid.
    ///
    /// `POSIX_SPAWN_SETPGROUP` is the point of using `posix_spawn` here rather
    /// than `Process`: the core launches vendor CLIs of its own, so a timeout
    /// has to signal the whole group. Without a group of its own the child would
    /// share this app's, and `kill(-pid, …)` would take down the app.
    private static func spawn(
        executableURL: URL,
        arguments: [String],
        stdoutPath: String,
        stderrPath: String,
        inputFD: Int32?
    ) throws -> pid_t {
        var actions: posix_spawn_file_actions_t?
        guard posix_spawn_file_actions_init(&actions) == 0 else {
            throw QuotaError.transport("Could not prepare output capture for quotamon")
        }
        defer { posix_spawn_file_actions_destroy(&actions) }

        if let inputFD {
            posix_spawn_file_actions_adddup2(&actions, inputFD, 0)
        } else {
            posix_spawn_file_actions_addopen(&actions, 0, "/dev/null", O_RDONLY, 0)
        }
        posix_spawn_file_actions_addopen(&actions, 1, stdoutPath, O_WRONLY | O_CREAT | O_TRUNC, 0o600)
        posix_spawn_file_actions_addopen(&actions, 2, stderrPath, O_WRONLY | O_CREAT | O_TRUNC, 0o600)

        var attributes: posix_spawnattr_t?
        guard posix_spawnattr_init(&attributes) == 0 else {
            throw QuotaError.transport("Could not prepare quotamon for launch")
        }
        defer { posix_spawnattr_destroy(&attributes) }
        posix_spawnattr_setflags(&attributes, Int16(POSIX_SPAWN_SETPGROUP))
        // 0 means "make the child its own group leader", so its pgid == its pid.
        posix_spawnattr_setpgroup(&attributes, 0)

        var argv: [UnsafeMutablePointer<CChar>?] =
            ([executableURL.path] + arguments).map { strdup($0) }
        argv.append(nil)
        defer { argv.forEach { free($0) } }

        var pid: pid_t = -1
        let result = posix_spawn(&pid, executableURL.path, &actions, &attributes, argv, environ)
        guard result == 0 else {
            throw QuotaError.transport(
                "Could not run quotamon — \(String(cString: strerror(result)))"
            )
        }
        return pid
    }

    /// Polls for the child until it exits or `deadline` passes; `nil` means the
    /// deadline won and the caller must kill the group.
    private static func wait(for pid: pid_t, until deadline: Date) -> Int32? {
        while true {
            var status: Int32 = 0
            let result = waitpid(pid, &status, WNOHANG)
            if result == -1, errno == EINTR { continue }
            // Anything but 0 means there is nothing left to wait for: the child
            // exited, or it vanished and no status will ever arrive.
            if result != 0 { return status }
            if Date() >= deadline { return nil }
            Thread.sleep(forTimeInterval: 0.02)
        }
    }

    /// SIGTERM, then SIGKILL, to the child's whole process group, then reaps it.
    ///
    /// The group is the point — killing only the leader leaves the vendor CLIs
    /// the core started running with no parent to stop them. A child that
    /// deliberately ignores SIGTERM is out of test scope: spawning a real
    /// signal-trapping process would make the suite depend on process
    /// scheduling, and the escalation below is a two-line unconditional path.
    private static func terminateGroup(_ pid: pid_t) {
        kill(-pid, SIGTERM)

        var status: Int32 = 0
        let graceDeadline = Date().addingTimeInterval(0.25)
        while Date() < graceDeadline {
            if waitpid(pid, &status, WNOHANG) != 0 { return }
            Thread.sleep(forTimeInterval: 0.01)
        }

        kill(-pid, SIGKILL)
        waitpid(pid, &status, 0)
    }

    /// The child's exit status, or `nil` when a signal killed it. Swift does not
    /// import the `WIFEXITED`/`WEXITSTATUS` macros, so the bit layout is spelled
    /// out here.
    private static func exitCode(from status: Int32) -> Int32? {
        guard status & 0x7f == 0 else { return nil }
        return (status >> 8) & 0xff
    }
}
