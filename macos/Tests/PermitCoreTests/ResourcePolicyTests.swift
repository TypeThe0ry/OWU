import Foundation
import XCTest
@testable import PermitCore

final class ResourcePolicyTests: XCTestCase {
    private let now = Date(timeIntervalSince1970: 2_000_000_000)

    func testAllowsExactPublicResourceMatch() throws {
        let endpoint = ResourceEndpoint(transport: .https, host: "wiki.example.com", port: 8443)
        let resource = AuthorizedResource(
            id: "res_wiki",
            displayName: "Public Wiki",
            visibility: .publicAccess,
            endpoints: [endpoint]
        )
        let result = try CatalogPolicy().authorize(
            RequestedDestination(transport: .https, host: "wiki.example.com", port: 8443, path: "/"),
            in: snapshot([resource]),
            now: now
        )
        XCTAssertEqual(result.resource.id, "res_wiki")
        XCTAssertEqual(result.endpoint, endpoint)
    }

    func testRestrictedResourceIsNotAvailableWithoutAccount() {
        let resource = AuthorizedResource(
            id: "res_private",
            displayName: "Restricted",
            visibility: .restricted,
            endpoints: [ResourceEndpoint(transport: .tcp, host: "db.example.com", port: 5432)]
        )
        XCTAssertThrowsError(
            try CatalogPolicy().authorize(
                RequestedDestination(transport: .tcp, host: "db.example.com", port: 5432),
                in: snapshot([resource]),
                now: now
            )
        ) { error in
            XCTAssertEqual(error as? StableAccessError, .resourceNotAuthorized)
        }
    }

    func testWrongPortAndUnknownHostFailClosed() {
        let resource = AuthorizedResource(
            id: "res_ssh",
            displayName: "Public SSH",
            visibility: .publicAccess,
            endpoints: [ResourceEndpoint(transport: .tcp, host: "ssh.example.com", port: 22)]
        )
        for destination in [
            RequestedDestination(transport: .tcp, host: "ssh.example.com", port: 23),
            RequestedDestination(transport: .tcp, host: "other.example.com", port: 22)
        ] {
            XCTAssertThrowsError(try CatalogPolicy().authorize(destination, in: snapshot([resource]), now: now))
        }
    }

    func testUnsignedAndExpiredCatalogsFailClosed() {
        let destination = RequestedDestination(transport: .tcp, host: "ssh.example.com", port: 22)
        let resource = AuthorizedResource(
            id: "res_ssh",
            displayName: "Public SSH",
            visibility: .publicAccess,
            endpoints: [ResourceEndpoint(transport: .tcp, host: "ssh.example.com", port: 22)]
        )
        let unsigned = ResourceCatalogSnapshot(
            resources: [resource], issuedAt: now, expiresAt: now.addingTimeInterval(60), signatureVerified: false
        )
        let expired = ResourceCatalogSnapshot(
            resources: [resource], issuedAt: now.addingTimeInterval(-120), expiresAt: now, signatureVerified: true
        )
        XCTAssertThrowsError(try CatalogPolicy().authorize(destination, in: unsigned, now: now))
        XCTAssertThrowsError(try CatalogPolicy().authorize(destination, in: expired, now: now))
    }

    func testPublicGatewayRejectsIPLiteralsEvenWhenCatalogContainsOne() {
        let resource = AuthorizedResource(
            id: "res_ip",
            displayName: "Unsafe IP",
            visibility: .publicAccess,
            endpoints: [ResourceEndpoint(transport: .tcp, host: "203.0.113.8", port: 22)]
        )
        XCTAssertThrowsError(
            try CatalogPolicy().authorize(
                RequestedDestination(transport: .tcp, host: "203.0.113.8", port: 22),
                in: snapshot([resource]),
                now: now
            )
        )
    }

    func testOwnedConnectorCanMatchExplicitPrivateIPButNeverLinkLocal() throws {
        let privateEndpoint = ResourceEndpoint(
            transport: .tcp,
            host: "10.20.0.15",
            port: 5432,
            connectionMode: .ownedConnector
        )
        let allowed = AuthorizedResource(
            id: "res_db",
            displayName: "Lab Database",
            visibility: .publicAccess,
            endpoints: [privateEndpoint]
        )
        XCTAssertNoThrow(
            try CatalogPolicy().authorize(
                RequestedDestination(transport: .tcp, host: "10.20.0.15", port: 5432),
                in: snapshot([allowed]),
                now: now
            )
        )

        let linkLocal = AuthorizedResource(
            id: "res_metadata",
            displayName: "Never Allowed",
            visibility: .publicAccess,
            endpoints: [ResourceEndpoint(
                transport: .http,
                host: "169.254.169.254",
                port: 80,
                connectionMode: .ownedConnector
            )]
        )
        XCTAssertThrowsError(
            try CatalogPolicy().authorize(
                RequestedDestination(transport: .http, host: "169.254.169.254", port: 80),
                in: snapshot([linkLocal]),
                now: now
            )
        )
    }

    private func snapshot(_ resources: [AuthorizedResource]) -> ResourceCatalogSnapshot {
        ResourceCatalogSnapshot(
            resources: resources,
            issuedAt: now,
            expiresAt: now.addingTimeInterval(300),
            signatureVerified: true
        )
    }
}
