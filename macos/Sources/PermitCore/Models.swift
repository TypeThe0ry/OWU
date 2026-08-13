import Foundation

public enum ResourceTransport: String, Codable, Sendable, CaseIterable {
    case http
    case https
    case tcp
}

public enum ResourceConnectionMode: String, Codable, Sendable {
    case publicGateway
    case ownedConnector
}

public enum ResourceVisibility: String, Codable, Sendable {
    case publicAccess = "public"
    case restricted
}

public struct ResourceEndpoint: Codable, Hashable, Sendable {
    public let transport: ResourceTransport
    public let host: String
    public let port: UInt16
    public let connectionMode: ResourceConnectionMode

    public init(
        transport: ResourceTransport,
        host: String,
        port: UInt16,
        connectionMode: ResourceConnectionMode = .publicGateway
    ) {
        self.transport = transport
        self.host = host.lowercased()
        self.port = port
        self.connectionMode = connectionMode
    }
}

public struct AuthorizedResource: Identifiable, Codable, Hashable, Sendable {
    public let id: String
    public let displayName: String
    public let visibility: ResourceVisibility
    public let endpoints: [ResourceEndpoint]
    public let webLaunchURL: String?

    public init(
        id: String,
        displayName: String,
        visibility: ResourceVisibility,
        endpoints: [ResourceEndpoint],
        webLaunchURL: String? = nil
    ) {
        self.id = id
        self.displayName = displayName
        self.visibility = visibility
        self.endpoints = endpoints
        self.webLaunchURL = webLaunchURL
    }
}

public struct RequestedDestination: Equatable, Sendable {
    public let transport: ResourceTransport
    public let host: String
    public let port: UInt16
    public let path: String?

    public init(transport: ResourceTransport, host: String, port: UInt16, path: String? = nil) {
        self.transport = transport
        self.host = host.lowercased()
        self.port = port
        self.path = path
    }
}

public struct RouteGrant: Sendable, Equatable {
    public let resourceID: String
    public let endpoint: ResourceEndpoint
    public let deviceThumbprint: String
    public let audience: String
    public let expiresAt: Date
    public let nonce: String
    public let compactToken: String

    public init(
        resourceID: String,
        endpoint: ResourceEndpoint,
        deviceThumbprint: String,
        audience: String,
        expiresAt: Date,
        nonce: String,
        compactToken: String
    ) {
        self.resourceID = resourceID
        self.endpoint = endpoint
        self.deviceThumbprint = deviceThumbprint
        self.audience = audience
        self.expiresAt = expiresAt
        self.nonce = nonce
        self.compactToken = compactToken
    }
}

public struct AuthorizedRoute: Sendable, Equatable {
    public let resource: AuthorizedResource
    public let endpoint: ResourceEndpoint
    public let grant: RouteGrant

    public init(resource: AuthorizedResource, endpoint: ResourceEndpoint, grant: RouteGrant) {
        self.resource = resource
        self.endpoint = endpoint
        self.grant = grant
    }
}

public enum StableAccessError: Error, Equatable, LocalizedError, Sendable {
    case malformedDestination
    case unsupportedScheme
    case credentialsNotAllowed
    case portNotAllowed
    case resourceNotAuthorized
    case ambiguousResource
    case catalogStale
    case grantExpired
    case grantMismatch
    case deviceUnavailable
    case proxyNotConfigured
    case systemTunnelNotEntitled

    public var errorDescription: String? {
        switch self {
        case .malformedDestination:
            return "Enter a valid URL or host:port."
        case .unsupportedScheme:
            return "Only HTTP, HTTPS, and approved TCP resources are supported."
        case .credentialsNotAllowed:
            return "URLs containing a username or password are not allowed."
        case .portNotAllowed:
            return "The requested port is not available for this resource."
        case .resourceNotAuthorized:
            return "This resource is not available to your account."
        case .ambiguousResource:
            return "More than one approved resource matches this destination. Refresh policy and try again."
        case .catalogStale:
            return "Your resource policy is out of date. Refresh it before connecting."
        case .grantExpired:
            return "The route grant expired. Request a new grant and try again."
        case .grantMismatch:
            return "The route grant does not match this device or resource."
        case .deviceUnavailable:
            return "This Mac does not have a usable device identity."
        case .proxyNotConfigured:
            return "The local proxy is not configured for this build."
        case .systemTunnelNotEntitled:
            return "System tunnel support is not enabled for this build."
        }
    }
}
