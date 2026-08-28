import Foundation

/// A forgiving JSON tree.
///
/// The provider endpoints here are private and undocumented, so their response
/// shapes can drift without notice. Rather than binding to one exact schema and
/// breaking outright, sources decode into this and pull out the fields they
/// recognise — including by searching nested objects for a known key.
public indirect enum JSONValue: Codable, Hashable, Sendable {
    case null
    case bool(Bool)
    case number(Double)
    case string(String)
    case array([JSONValue])
    case object([String: JSONValue])

    public init(from decoder: Decoder) throws {
        let c = try decoder.singleValueContainer()
        if c.decodeNil() { self = .null }
        else if let v = try? c.decode(Bool.self) { self = .bool(v) }
        else if let v = try? c.decode(Double.self) { self = .number(v) }
        else if let v = try? c.decode(String.self) { self = .string(v) }
        else if let v = try? c.decode([JSONValue].self) { self = .array(v) }
        else if let v = try? c.decode([String: JSONValue].self) { self = .object(v) }
        else {
            throw DecodingError.dataCorruptedError(in: c, debugDescription: "Unrecognised JSON value")
        }
    }

    public func encode(to encoder: Encoder) throws {
        var c = encoder.singleValueContainer()
        switch self {
        case .null: try c.encodeNil()
        case .bool(let v): try c.encode(v)
        case .number(let v): try c.encode(v)
        case .string(let v): try c.encode(v)
        case .array(let v): try c.encode(v)
        case .object(let v): try c.encode(v)
        }
    }

    public static func parse(_ data: Data) throws -> JSONValue {
        try JSONDecoder().decode(JSONValue.self, from: data)
    }

    // MARK: - Access

    public subscript(key: String) -> JSONValue? {
        guard case .object(let o) = self, let v = o[key], v != .null else { return nil }
        return v
    }

    public var double: Double? {
        switch self {
        case .number(let v): v
        case .string(let s): Double(s)
        case .bool(let b): b ? 1 : 0
        default: nil
        }
    }

    public var int: Int? { double.map(Int.init) }

    public var string: String? {
        switch self {
        case .string(let s): s
        case .number(let v): String(v)
        case .bool(let b): String(b)
        default: nil
        }
    }

    public var bool: Bool? {
        switch self {
        case .bool(let b): b
        case .number(let v): v != 0
        case .string(let s): Bool(s)
        default: nil
        }
    }

    /// Interprets the value as a timestamp: epoch seconds (or milliseconds) as a
    /// number, or an ISO-8601 string.
    public var date: Date? {
        if case .string(let s) = self { return Date.fromISO8601(s) }
        guard let v = double, v > 0 else { return nil }
        // Anything past year ~2286 in seconds is really milliseconds.
        return Date(timeIntervalSince1970: v > 100_000_000_000 ? v / 1000 : v)
    }

    /// First value found for `key`, searching this node then descending
    /// breadth-first. Lets a source say "find `five_hour` wherever it lives".
    ///
    /// - Important: only for documents where a key name is unambiguous. Sibling
    ///   subtrees are searched in dictionary order, which is not stable, so on a
    ///   document containing the same key in several places this returns an
    ///   arbitrary one. Never use it to pull credentials out of a blob that
    ///   holds more than one service's tokens — address those by explicit path.
    public func firstValue(forKey key: String) -> JSONValue? {
        var queue: [JSONValue] = [self]
        while !queue.isEmpty {
            let node = queue.removeFirst()
            switch node {
            case .object(let o):
                if let hit = o[key], hit != .null { return hit }
                queue.append(contentsOf: o.values)
            case .array(let a):
                queue.append(contentsOf: a)
            default:
                break
            }
        }
        return nil
    }

    /// First non-nil value among several candidate key spellings.
    public func firstValue(forAnyKey keys: [String]) -> JSONValue? {
        for key in keys {
            if let hit = self[key] ?? firstValue(forKey: key) { return hit }
        }
        return nil
    }
}

extension Date {
    static func fromISO8601(_ s: String) -> Date? {
        let withFraction = ISO8601DateFormatter()
        withFraction.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        if let d = withFraction.date(from: s) { return d }
        let plain = ISO8601DateFormatter()
        plain.formatOptions = [.withInternetDateTime]
        return plain.date(from: s)
    }
}
