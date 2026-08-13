import Foundation
import XCTest
@testable import PermitCore

final class ProxyAuthorizationCoordinatorTests: XCTestCase {
    func testUnknownDestinationNeverRequestsGrant() async throws {
        let endpoint = ResourceEndpoint(transport: .tcp, host: "ssh.example.com", port: 22)
        let catalog = catalogWith(endpoint: endpoint)
        let resolver = RecordingGrantResolver(endpoint: endpoint)
        let coordinator = ProxyAuthorizationCoordinator(
            catalog: catalog,
            expectedSessionCredential: Data("secret".utf8),
            identity: MockIdentity(),
            grantResolver: resolver,
            gatewayAudience: "gateway.example.com"
        )

        do {
            _ = try await coordinator.authorize(
                destination: RequestedDestination(transport: .tcp, host: "unknown.example.com", port: 22),
                presentedSessionCredential: Data("secret".utf8)
            )
            XCTFail("Unknown destination should fail closed")
        } catch {
            XCTAssertEqual(error as? StableAccessError, .resourceNotAuthorized)
        }
        let rejectedCallCount = await resolver.callCount
        XCTAssertEqual(rejectedCallCount, 0)
    }

    func testApprovedDestinationProducesResourceBoundOpenRequest() async throws {
        let endpoint = ResourceEndpoint(transport: .tcp, host: "ssh.example.com", port: 22)
        let resolver = RecordingGrantResolver(endpoint: endpoint)
        let coordinator = ProxyAuthorizationCoordinator(
            catalog: catalogWith(endpoint: endpoint),
            expectedSessionCredential: Data("secret".utf8),
            identity: MockIdentity(),
            grantResolver: resolver,
            gatewayAudience: "gateway.example.com"
        )

        let request = try await coordinator.authorize(
            destination: RequestedDestination(transport: .tcp, host: "ssh.example.com", port: 22),
            presentedSessionCredential: Data("secret".utf8)
        )
        XCTAssertEqual(request.resourceID, "res_public")
        XCTAssertEqual(request.port, 22)
        XCTAssertEqual(request.routeGrant, "signed-token")
        XCTAssertFalse(request.deviceProof.isEmpty)
        let approvedCallCount = await resolver.callCount
        XCTAssertEqual(approvedCallCount, 1)
    }

    private func catalogWith(endpoint: ResourceEndpoint) -> ResourceCatalogSnapshot {
        ResourceCatalogSnapshot(
            resources: [AuthorizedResource(
                id: "res_public",
                displayName: "Public SSH",
                visibility: .publicAccess,
                endpoints: [endpoint]
            )],
            issuedAt: Date().addingTimeInterval(-10),
            expiresAt: Date().addingTimeInterval(300),
            signatureVerified: true
        )
    }
}

private actor MockIdentity: DeviceIdentityProviding {
    func prepareIdentity() async throws -> DeviceIdentityReference {
        DeviceIdentityReference(
            keyTag: "test",
            publicKey: Data("public".utf8),
            thumbprint: "device-a",
            isHardwareBacked: false
        )
    }

    func sign(_ challenge: Data) async throws -> Data {
        Data("signature".utf8)
    }

    func deleteIdentity() async throws {}
}

private actor RecordingGrantResolver: RouteGrantResolving {
    private(set) var callCount = 0
    private let endpoint: ResourceEndpoint

    init(endpoint: ResourceEndpoint) {
        self.endpoint = endpoint
    }

    func resolveGrant(
        resourceID: String,
        endpoint: ResourceEndpoint,
        deviceThumbprint: String
    ) async throws -> RouteGrant {
        callCount += 1
        return RouteGrant(
            resourceID: resourceID,
            endpoint: endpoint,
            deviceThumbprint: deviceThumbprint,
            audience: "gateway.example.com",
            expiresAt: Date().addingTimeInterval(60),
            nonce: "nonce",
            compactToken: "signed-token"
        )
    }
}
