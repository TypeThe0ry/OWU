import Foundation
import XCTest
@testable import PermitCore

final class GrantAndTunnelTests: XCTestCase {
    func testGrantMustMatchDeviceResourceEndpointAndAudience() throws {
        let endpoint = ResourceEndpoint(transport: .tcp, host: "ssh.example.com", port: 22)
        let resource = AuthorizedResource(
            id: "res_ssh",
            displayName: "Public SSH",
            visibility: .publicAccess,
            endpoints: [endpoint]
        )
        let grant = RouteGrant(
            resourceID: resource.id,
            endpoint: endpoint,
            deviceThumbprint: "device-a",
            audience: "gateway.example.com",
            expiresAt: Date().addingTimeInterval(60),
            nonce: "nonce",
            compactToken: "signed-token"
        )
        XCTAssertNoThrow(
            try RouteGrantValidator().validate(
                grant,
                resource: resource,
                endpoint: endpoint,
                deviceThumbprint: "device-a",
                audience: "gateway.example.com"
            )
        )
        XCTAssertThrowsError(
            try RouteGrantValidator().validate(
                grant,
                resource: resource,
                endpoint: endpoint,
                deviceThumbprint: "device-b",
                audience: "gateway.example.com"
            )
        )
    }

    func testTunnelConfigurationRejectsFullRoutesAndDefaultDNS() {
        XCTAssertThrowsError(
            try ApprovedTunnelConfiguration(overlayCIDRs: ["0.0.0.0/0"], privateDNSSuffixes: ["permit.example"])
        )
        XCTAssertThrowsError(
            try ApprovedTunnelConfiguration(overlayCIDRs: ["100.96.0.0/16"], privateDNSSuffixes: [""])
        )
    }

    func testLoopbackConfigurationCapsUnsafeValues() {
        XCTAssertNoThrow(
            try LoopbackListenerConfiguration(kind: .socks5, address: .ipv4, port: 1080)
        )
        XCTAssertThrowsError(
            try LoopbackListenerConfiguration(
                kind: .httpConnect,
                address: .ipv4,
                port: 8080,
                maximumHandshakeBytes: 1_000_000
            )
        )
    }
}
