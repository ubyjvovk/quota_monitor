import Foundation

public enum QuotaError: LocalizedError, Sendable {
    case notConfigured(String)
    case noDataFound(String)
    case unauthorized(String)
    case transport(String)
    case malformed(String)

    public var errorDescription: String? {
        switch self {
        case .notConfigured(let m): m
        case .noDataFound(let m): m
        case .unauthorized(let m): m
        case .transport(let m): m
        case .malformed(let m): m
        }
    }
}
