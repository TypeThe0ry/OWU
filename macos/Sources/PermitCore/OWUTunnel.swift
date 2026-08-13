@preconcurrency import Foundation
#if canImport(FoundationNetworking)
import FoundationNetworking
#endif

public enum OWUConfigurationError: Error, Equatable, LocalizedError, Sendable {
    case invalidServer
    case invalidUsername
    case invalidPassword
    case invalidResourceID
    case invalidLocalPort

    public var errorDescription: String? {
        switch self {
        case .invalidServer: return "Enter an HTTPS OWU server address."
        case .invalidUsername: return "Enter the browser access username."
        case .invalidPassword: return "Enter the browser access password."
        case .invalidResourceID: return "The tunnel resource ID is invalid."
        case .invalidLocalPort: return "Choose a local port from 1 through 65535."
        }
    }
}

public struct OWUServerConfiguration: Equatable, Sendable {
    public let baseURL: URL
    public let username: String
    public let password: String
    public let certificateSHA256: String?

    public init(
        baseURL: URL,
        username: String,
        password: String,
        certificateSHA256: String? = nil
    ) throws {
        guard baseURL.scheme?.lowercased() == "https",
              baseURL.host != nil,
              baseURL.user == nil,
              baseURL.password == nil,
              baseURL.query == nil,
              baseURL.fragment == nil else {
            throw OWUConfigurationError.invalidServer
        }
        let cleanUsername = username.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !cleanUsername.isEmpty, !cleanUsername.contains(":") else {
            throw OWUConfigurationError.invalidUsername
        }
        guard password.utf8.count >= 20 else {
            throw OWUConfigurationError.invalidPassword
        }
        self.baseURL = baseURL
        self.username = cleanUsername
        self.password = password
        let normalizedPin = certificateSHA256?
            .filter { $0.isHexDigit }
            .lowercased()
        self.certificateSHA256 = normalizedPin?.isEmpty == false ? normalizedPin : nil
    }

    public func tunnelRequest(resourceID: String) throws -> URLRequest {
        guard OWUTunnelPreset.isValidResourceID(resourceID) else {
            throw OWUConfigurationError.invalidResourceID
        }
        guard var components = URLComponents(url: baseURL, resolvingAgainstBaseURL: false) else {
            throw OWUConfigurationError.invalidServer
        }
        components.scheme = "wss"
        components.path = "/tunnel/\(resourceID)"
        components.query = nil
        components.fragment = nil
        guard let tunnelURL = components.url else {
            throw OWUConfigurationError.invalidServer
        }
        var request = URLRequest(url: tunnelURL)
        request.timeoutInterval = 20
        let basic = Data("\(username):\(password)".utf8).base64EncodedString()
        request.setValue("Basic \(basic)", forHTTPHeaderField: "Authorization")
        request.setValue(password, forHTTPHeaderField: "X-OWU-Tunnel-Key")
        request.setValue("no-store", forHTTPHeaderField: "Cache-Control")
        return request
    }
}

public struct OWUTunnelPreset: Identifiable, Equatable, Sendable {
    public let id: String
    public let name: String
    public let symbol: String
    public let localPort: UInt16
    public let usage: String

    public init(id: String, name: String, symbol: String, localPort: UInt16, usage: String) throws {
        guard Self.isValidResourceID(id) else { throw OWUConfigurationError.invalidResourceID }
        guard localPort > 0 else { throw OWUConfigurationError.invalidLocalPort }
        self.id = id
        self.name = name
        self.symbol = symbol
        self.localPort = localPort
        self.usage = usage
    }

    public static func isValidResourceID(_ value: String) -> Bool {
        guard !value.isEmpty, value.utf8.count <= 64,
              (value.first?.isLetter == true || value.first?.isNumber == true) else { return false }
        return value.allSatisfy { $0.isLowercase || $0.isNumber || $0 == "_" || $0 == "-" }
    }

    public static let ssh = try! OWUTunnelPreset(
        id: "ssh",
        name: "SSH",
        symbol: "terminal",
        localPort: 2222,
        usage: "ssh -p 2222 user@127.0.0.1"
    )

    public static let minecraft = try! OWUTunnelPreset(
        id: "minecraft",
        name: "Minecraft",
        symbol: "cube",
        localPort: 25565,
        usage: "127.0.0.1:25565"
    )

    public static let defaults: [OWUTunnelPreset] = [.ssh, .minecraft]
}

public enum OWUTunnelState: Equatable, Sendable {
    case stopped
    case starting
    case ready
    case failed(String)
}
