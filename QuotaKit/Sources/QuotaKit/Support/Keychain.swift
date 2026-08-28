import Foundation
import Security

public enum Keychain {
    /// Reads a generic-password item's data by service name.
    ///
    /// The first read of an item another app created triggers the system's
    /// "allow access?" prompt. Choosing "Always Allow" makes it silent after that.
    public static func genericPassword(service: String) throws -> Data {
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
}
