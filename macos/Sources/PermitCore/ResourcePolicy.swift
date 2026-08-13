import Foundation

public struct ResourceCatalogSnapshot: Sendable, Equatable {
    public let resources: [AuthorizedResource]
    public let issuedAt: Date
    public let expiresAt: Date
    public let signatureVerified: Bool

    public init(
        resources: [AuthorizedResource],
        issuedAt: Date,
        expiresAt: Date,
        signatureVerified: Bool
    ) {
        self.resources = resources
        self.issuedAt = issuedAt
        self.expiresAt = expiresAt
        self.signatureVerified = signatureVerified
    }
}

public struct CatalogPolicy: Sendable {
    public init() {}

    public func authorize(
        _ destination: RequestedDestination,
        in catalog: ResourceCatalogSnapshot,
        now: Date = Date()
    ) throws -> (resource: AuthorizedResource, endpoint: ResourceEndpoint) {
        guard catalog.signatureVerified, catalog.expiresAt > now else {
            throw StableAccessError.catalogStale
        }

        var matches: [(AuthorizedResource, ResourceEndpoint)] = []
        for resource in catalog.resources where resource.visibility == .publicAccess {
            for endpoint in resource.endpoints {
                if endpoint.host == destination.host,
                   endpoint.port == destination.port,
                   endpoint.transport == destination.transport {
                    matches.append((resource, endpoint))
                }
            }
        }

        guard !matches.isEmpty else { throw StableAccessError.resourceNotAuthorized }
        guard matches.count == 1 else { throw StableAccessError.ambiguousResource }
        let match = matches[0]
        try enforceAddressPolicy(match.1)
        return (match.0, match.1)
    }

    private func enforceAddressPolicy(_ endpoint: ResourceEndpoint) throws {
        guard let literal = AddressLiteral(endpoint.host) else { return }

        if literal.isAlwaysForbidden {
            throw StableAccessError.resourceNotAuthorized
        }
        if endpoint.connectionMode == .publicGateway {
            throw StableAccessError.resourceNotAuthorized
        }
    }
}

private struct AddressLiteral {
    enum Kind {
        case ipv4([UInt8])
        case ipv6(String)
    }

    let kind: Kind

    init?(_ host: String) {
        if host.contains(":") {
            kind = .ipv6(host.lowercased())
            return
        }
        let parts = host.split(separator: ".", omittingEmptySubsequences: false)
        guard parts.count == 4 else { return nil }
        var octets: [UInt8] = []
        for part in parts {
            guard !part.isEmpty,
                  part.allSatisfy({ $0.isASCII && $0.isNumber }),
                  let octet = UInt8(part) else {
                return nil
            }
            octets.append(octet)
        }
        kind = .ipv4(octets)
    }

    var isAlwaysForbidden: Bool {
        switch kind {
        case let .ipv4(octets):
            let first = octets[0]
            let second = octets[1]
            return first == 0
                || first == 127
                || (first == 169 && second == 254)
                || first >= 224
        case let .ipv6(value):
            return value == "::"
                || value == "::1"
                || value.hasPrefix("fe8")
                || value.hasPrefix("fe9")
                || value.hasPrefix("fea")
                || value.hasPrefix("feb")
                || value.hasPrefix("ff")
                || value.hasPrefix("::ffff:")
        }
    }
}

public struct RouteGrantValidator: Sendable {
    public init() {}

    public func validate(
        _ grant: RouteGrant,
        resource: AuthorizedResource,
        endpoint: ResourceEndpoint,
        deviceThumbprint: String,
        audience: String,
        now: Date = Date()
    ) throws {
        guard grant.expiresAt > now else { throw StableAccessError.grantExpired }
        guard grant.resourceID == resource.id,
              grant.endpoint == endpoint,
              grant.deviceThumbprint == deviceThumbprint,
              grant.audience == audience,
              !grant.nonce.isEmpty,
              !grant.compactToken.isEmpty else {
            throw StableAccessError.grantMismatch
        }
    }
}
