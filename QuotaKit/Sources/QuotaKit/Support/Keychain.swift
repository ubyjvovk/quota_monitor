import Foundation
import Security

/// One strategy for reading a generic-password item. Injectable so tests
/// never touch the real Keychain.
public struct KeychainLookup: Sendable {
    /// A short name identifying the lookup strategy.
    public let name: String

    /// Reads an item's data using its Keychain service name.
    public let read: @Sendable (String) throws -> Data

    /// Creates a named generic-password lookup strategy.
    public init(name: String, read: @escaping @Sendable (String) throws -> Data) {
        self.name = name
        self.read = read
    }
}

public enum Keychain {
    /// The `security` CLI, and only it. `SecItemCopyMatching` is deliberately
    /// not in this list: for an item another app owns it is either refused
    /// outright or answered with an interactive "allow access?" dialog, and a
    /// command-line tool must never block on a GUI prompt.
    public static let defaultLookups: [KeychainLookup] = [securityCLI]

    /// Shells out to `/usr/bin/security`. The default strategy.
    public static let securityCLI = KeychainLookup(name: "security") { service in
        let process = Process()
        let standardOutput = Pipe()
        process.executableURL = URL(fileURLWithPath: "/usr/bin/security")
        process.arguments = ["find-generic-password", "-s", service, "-w"]
        process.standardOutput = standardOutput
        process.standardError = FileHandle.nullDevice

        do {
            try process.run()
        } catch {
            throw QuotaError.transport("`security` could not launch for '\(service)'")
        }

        let output = standardOutput.fileHandleForReading.readDataToEndOfFile()
        process.waitUntilExit()

        guard process.terminationStatus == 0 else {
            switch process.terminationStatus {
            case 44:
                throw QuotaError.notConfigured("No Keychain item named '\(service)'")
            case 51, 128:
                throw QuotaError.unauthorized("Keychain access to '\(service)' was denied")
            default:
                throw QuotaError.transport(
                    "`security` failed for '\(service)' (exit \(process.terminationStatus))"
                )
            }
        }

        var value = String(decoding: output, as: UTF8.self)
        while value.last?.isWhitespace == true {
            value.removeLast()
        }
        guard !value.isEmpty else {
            throw QuotaError.malformed("Keychain item '\(service)' held no data")
        }
        return Data(value.utf8)
    }

    /// The framework API. Not used by default — see `defaultLookups`. Kept as
    /// an opt-in for a signed app bundle that holds the item's ACL.
    public static let secItem = KeychainLookup(name: "SecItem") { service in
        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecReturnData as String: true,
            kSecMatchLimit as String: kSecMatchLimitOne,
        ]

        var item: CFTypeRef?
        let status = SecItemCopyMatching(query as CFDictionary, &item)

        switch status {
        case errSecSuccess:
            guard let data = item as? Data else {
                throw QuotaError.malformed("Keychain item '\(service)' held no data")
            }
            return data
        case errSecItemNotFound:
            throw QuotaError.notConfigured("No Keychain item named '\(service)'")
        case errSecUserCanceled, errSecAuthFailed, errSecInteractionNotAllowed:
            throw QuotaError.unauthorized("Keychain access to '\(service)' was denied")
        default:
            throw QuotaError.transport("Keychain read failed for '\(service)' (OSStatus \(status))")
        }
    }

    /// Reads a generic-password item's data by service name.
    ///
    /// Lookups run in order until one succeeds. If every lookup fails, the most
    /// actionable failure is thrown, with earlier lookups winning ties.
    public static func genericPassword(
        service: String,
        lookups: [KeychainLookup] = defaultLookups
    ) throws -> Data {
        guard !lookups.isEmpty else {
            throw QuotaError.notConfigured("No Keychain lookup configured")
        }

        var bestFailure: (error: any Error, priority: Int)?
        for lookup in lookups {
            do {
                return try lookup.read(service)
            } catch {
                let priority = (error as? QuotaError)?.reportingPriority ?? 2
                if bestFailure.map({ priority > $0.priority }) ?? true {
                    bestFailure = (error, priority)
                }
            }
        }

        guard let bestFailure else {
            throw QuotaError.notConfigured("No Keychain lookup configured")
        }
        throw bestFailure.error
    }
}
