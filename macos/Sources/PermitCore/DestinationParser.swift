import Foundation

public struct DestinationParser: Sendable {
    public init() {}

    public func parse(_ rawValue: String) throws -> RequestedDestination {
        let value = rawValue.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !value.isEmpty, value.utf8.count <= 2_048 else {
            throw StableAccessError.malformedDestination
        }
        guard value.rangeOfCharacter(from: .controlCharacters) == nil, !value.contains("\\") else {
            throw StableAccessError.malformedDestination
        }

        if value.contains("://") {
            return try parseURL(value)
        }
        return try parseAuthority(value)
    }

    private func parseURL(_ value: String) throws -> RequestedDestination {
        guard let components = URLComponents(string: value),
              let schemeValue = components.scheme?.lowercased() else {
            throw StableAccessError.malformedDestination
        }

        let transport: ResourceTransport
        let defaultPort: UInt16
        switch schemeValue {
        case "http":
            transport = .http
            defaultPort = 80
        case "https":
            transport = .https
            defaultPort = 443
        default:
            throw StableAccessError.unsupportedScheme
        }
        guard components.user == nil, components.password == nil else {
            throw StableAccessError.credentialsNotAllowed
        }
        guard let hostValue = components.host, !hostValue.isEmpty else {
            throw StableAccessError.malformedDestination
        }

        let explicitPort = try explicitPortText(in: value)
        let port = try checkedPort(explicitPort, defaultValue: defaultPort)
        let path = components.percentEncodedPath.isEmpty ? "/" : components.percentEncodedPath
        return RequestedDestination(
            transport: transport,
            host: try normalizeHost(hostValue),
            port: port,
            path: path
        )
    }

    private func parseAuthority(_ value: String) throws -> RequestedDestination {
        guard !value.contains("/"), !value.contains("?"), !value.contains("#"), !value.contains("@") else {
            throw StableAccessError.malformedDestination
        }

        let host: String
        let portText: String
        if value.hasPrefix("[") {
            guard let closing = value.firstIndex(of: "]"),
                  value.index(after: closing) < value.endIndex,
                  value[value.index(after: closing)] == ":" else {
                throw StableAccessError.malformedDestination
            }
            host = String(value[value.index(after: value.startIndex)..<closing])
            portText = String(value[value.index(closing, offsetBy: 2)...])
        } else {
            guard let separator = value.lastIndex(of: ":"),
                  !value[..<separator].contains(":"),
                  separator != value.startIndex else {
                throw StableAccessError.malformedDestination
            }
            host = String(value[..<separator])
            portText = String(value[value.index(after: separator)...])
        }

        guard !host.isEmpty else { throw StableAccessError.malformedDestination }
        return RequestedDestination(
            transport: .tcp,
            host: try normalizeHost(host),
            port: try checkedPort(portText, defaultValue: nil),
            path: nil
        )
    }

    private func checkedPort(_ text: String?, defaultValue: UInt16?) throws -> UInt16 {
        guard let text else {
            guard let defaultValue else { throw StableAccessError.portNotAllowed }
            return defaultValue
        }
        guard !text.isEmpty,
              text.allSatisfy({ $0.isASCII && $0.isNumber }),
              let numeric = UInt16(text),
              numeric > 0 else {
            throw StableAccessError.portNotAllowed
        }
        return numeric
    }

    private func explicitPortText(in value: String) throws -> String? {
        guard let schemeRange = value.range(of: "://") else { return nil }
        let authorityAndRest = value[schemeRange.upperBound...]
        let authority = authorityAndRest.prefix { !"/?#".contains($0) }
        guard !authority.isEmpty else { throw StableAccessError.malformedDestination }

        if authority.hasPrefix("[") {
            guard let closing = authority.firstIndex(of: "]") else {
                throw StableAccessError.malformedDestination
            }
            let remainder = authority[authority.index(after: closing)...]
            if remainder.isEmpty { return nil }
            guard remainder.first == ":" else { throw StableAccessError.malformedDestination }
            return String(remainder.dropFirst())
        }

        guard let separator = authority.lastIndex(of: ":") else { return nil }
        return String(authority[authority.index(after: separator)...])
    }

    private func normalizeHost(_ host: String) throws -> String {
        var normalized = host.lowercased()
        while normalized.hasSuffix(".") { normalized.removeLast() }
        guard !normalized.isEmpty,
              !normalized.hasPrefix("."),
              normalized.rangeOfCharacter(from: .whitespacesAndNewlines) == nil else {
            throw StableAccessError.malformedDestination
        }
        return normalized
    }
}
