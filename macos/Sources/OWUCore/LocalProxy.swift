import Foundation

public enum LocalProxyKind: String, CaseIterable, Sendable {
    case socks5 = "SOCKS5"
    case httpConnect = "HTTP CONNECT"
}

public enum LoopbackAddress: String, CaseIterable, Sendable {
    case ipv4 = "127.0.0.1"
    case ipv6 = "::1"
}

public struct LoopbackListenerConfiguration: Equatable, Sendable {
    public let kind: LocalProxyKind
    public let address: LoopbackAddress
    public let port: UInt16
    public let maximumHandshakeBytes: Int
    public let maximumConcurrentFlows: Int
    public let handshakeTimeout: TimeInterval

    public init(
        kind: LocalProxyKind,
        address: LoopbackAddress,
        port: UInt16,
        maximumHandshakeBytes: Int = 32 * 1_024,
        maximumConcurrentFlows: Int = 100,
        handshakeTimeout: TimeInterval = 10
    ) throws {
        guard port > 0,
              maximumHandshakeBytes > 0,
              maximumHandshakeBytes <= 64 * 1_024,
              maximumConcurrentFlows > 0,
              maximumConcurrentFlows <= 1_000,
              handshakeTimeout > 0,
              handshakeTimeout <= 60 else {
            throw StableAccessError.proxyNotConfigured
        }
        self.kind = kind
        self.address = address
        self.port = port
        self.maximumHandshakeBytes = maximumHandshakeBytes
        self.maximumConcurrentFlows = maximumConcurrentFlows
        self.handshakeTimeout = handshakeTimeout
    }
}

public enum LocalProxyState: Equatable, Sendable {
    case stopped
    case starting
    case ready
    case stopping
    case failed(message: String)
}

public protocol LocalProxyServicing: Sendable {
    func start() async throws
    func stop() async
    func currentState() async -> LocalProxyState
}

public actor FailClosedLocalProxyService: LocalProxyServicing {
    private var state: LocalProxyState = .stopped

    public init() {}

    public func start() async throws {
        state = .failed(message: StableAccessError.proxyNotConfigured.localizedDescription)
        throw StableAccessError.proxyNotConfigured
    }

    public func stop() async {
        state = .stopped
    }

    public func currentState() async -> LocalProxyState {
        state
    }
}

public protocol RouteGrantResolving: Sendable {
    func resolveGrant(
        resourceID: String,
        endpoint: ResourceEndpoint,
        deviceThumbprint: String
    ) async throws -> RouteGrant
}

public struct GatewayOpenRequest: Sendable, Equatable {
    public let protocolVersion: UInt8
    public let resourceID: String
    public let port: UInt16
    public let routeGrant: String
    public let deviceProof: Data
    public let traceID: String

    public init(
        protocolVersion: UInt8 = 1,
        resourceID: String,
        port: UInt16,
        routeGrant: String,
        deviceProof: Data,
        traceID: String
    ) {
        self.protocolVersion = protocolVersion
        self.resourceID = resourceID
        self.port = port
        self.routeGrant = routeGrant
        self.deviceProof = deviceProof
        self.traceID = traceID
    }
}

public protocol GatewayFlowOpening: Sendable {
    func open(_ request: GatewayOpenRequest) async throws
}

public actor ProxyAuthorizationCoordinator {
    private let catalog: ResourceCatalogSnapshot
    private let expectedSessionCredential: Data
    private let identity: any DeviceIdentityProviding
    private let grantResolver: any RouteGrantResolving
    private let gatewayAudience: String

    public init(
        catalog: ResourceCatalogSnapshot,
        expectedSessionCredential: Data,
        identity: any DeviceIdentityProviding,
        grantResolver: any RouteGrantResolving,
        gatewayAudience: String
    ) {
        self.catalog = catalog
        self.expectedSessionCredential = expectedSessionCredential
        self.identity = identity
        self.grantResolver = grantResolver
        self.gatewayAudience = gatewayAudience
    }

    public func authorize(
        destination: RequestedDestination,
        presentedSessionCredential: Data,
        now: Date = Date()
    ) async throws -> GatewayOpenRequest {
        guard constantTimeEqual(presentedSessionCredential, expectedSessionCredential) else {
            throw StableAccessError.resourceNotAuthorized
        }

        let match = try CatalogPolicy().authorize(destination, in: catalog, now: now)
        let device = try await identity.prepareIdentity()
        let grant = try await grantResolver.resolveGrant(
            resourceID: match.resource.id,
            endpoint: match.endpoint,
            deviceThumbprint: device.thumbprint
        )
        try RouteGrantValidator().validate(
            grant,
            resource: match.resource,
            endpoint: match.endpoint,
            deviceThumbprint: device.thumbprint,
            audience: gatewayAudience,
            now: now
        )

        let proof = try await identity.sign(Data(grant.compactToken.utf8))
        return GatewayOpenRequest(
            resourceID: match.resource.id,
            port: match.endpoint.port,
            routeGrant: grant.compactToken,
            deviceProof: proof,
            traceID: UUID().uuidString.lowercased()
        )
    }

    private func constantTimeEqual(_ left: Data, _ right: Data) -> Bool {
        guard left.count == right.count else { return false }
        return zip(left, right).reduce(UInt8(0)) { partial, pair in
            partial | (pair.0 ^ pair.1)
        } == 0
    }
}
